package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"golang.org/x/oauth2"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"

	log "github.com/sirupsen/logrus"

	helm "github.com/OpusCapita/buhtig-s8k/pkg/helm"
	konnect "github.com/OpusCapita/buhtig-s8k/pkg/konnect"
)

const (
	labelSelector = "opuscapita.com/buhtig-s8k=true"

	githubURLAnnotationName   = "opuscapita.com/github-source-url"
	helmReleaseAnnotationName = "opuscapita.com/helm-release"

	ghTokenEnv = "GH_TOKEN"
)

var k8sConfig *rest.Config
var k8sClient *kubernetes.Clientset

func init() {
	log.SetLevel(log.DebugLevel)
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	// assert if required env variables are defined
	assertEnv(ghTokenEnv)

	// setup K8s client
	var err error

	// get k8s connection config
	k8sConfig, err = konnect.NewConfig()
	if err != nil {
		panic(err)
	}

	// get k8s client for config
	k8sClient, err = konnect.NewClient(k8sConfig)
	if err != nil {
		panic(err)
	}
}

func main() {
	// set buffer of 1 to enable non-blocking send before any consumers are ready
	start := make(chan struct{}, 1)
	errReport := make(chan error, 1)

	// trigger first iteration
	start <- struct{}{}

	for {
		// main goroutine designed to run infinitely
		// it can return only in case of panic inside it; outer loop will then start new iteration over again
		go func() {
			// catch panic and send error to special channel instead of halting program
			defer func() {
				var err error
				if r := recover(); r != nil {
					switch t := r.(type) {
					case string:
						err = errors.New(t)
					case error:
						err = t
					default:
						err = fmt.Errorf("%v", t)
					}
				}
				// report exception to errReport channel
				errReport <- err
			}()

			for {
				select {
				// this blocks until 'start' channel receives a value
				case <-start:
					log.Info("Starting new iteration")

					// compose channels returned by function-per-step in direction inner-to-outer (https://en.wikipedia.org/wiki/Function_composition)
					// all these functions have the same signature f(chan *namespace) -> chan *namespace
					// therefore can be easily composed
					// items in the resulting channel are those namespaces which completed all consequent steps in workflow
					processed := filterNamespaceDeleted(
						filterHelmReleaseDeletedIfNeeded(
							filterWithDeletedBranches(
								getNamespaces(),
							),
						),
					)

					// block until 'processed' channel is closed
					for ns := range processed {
						ns.logger().Debug("Namespace completed")
					}

					log.Debug("All namespaces completed, time to reschedule")
					go func() {
						log.Debug("Sleep")
						<-time.After(time.Minute)
						log.Debug("Reschedule")
						start <- struct{}{}
					}()
				}
			}
		}()

		err := <-errReport
		log.Error(err)
	}
}

// wrap type corev1.Namespace with our own name 'namespace' to enable custom methods
// data-wise it'll be the same data, but provide possibility to use custom instance methods,
// e.g. calculate github source url or helm release from namespace's annotations
type namespace corev1.Namespace

func (ns *namespace) Name() string {
	return ns.ObjectMeta.Name
}

func (ns *namespace) logger() *log.Entry {
	return log.WithFields(log.Fields{"namespace": ns.Name()})
}

func (ns *namespace) GithubSourceURL() (string, error) {
	githubURL, ok := ns.ObjectMeta.Annotations[githubURLAnnotationName]
	if !ok {
		return "", fmt.Errorf("Annotation '%s' not set", githubURLAnnotationName)
	}
	ns.logger().Debug(fmt.Sprintf("%s = %s", githubURLAnnotationName, githubURL))
	return githubURL, nil
}

func (ns *namespace) HelmRelease() (string, error) {
	helmRelease, ok := ns.ObjectMeta.Annotations[helmReleaseAnnotationName]
	if !ok {
		return "", fmt.Errorf("Annotation '%s' not set", helmReleaseAnnotationName)
	}
	return helmRelease, nil
}

// implement Stringer type to enable usage of namespace type in string context (print to stdout, concat string, etc.)
func (ns *namespace) String() string {
	return ns.Name()
}

// getNamespaces returns a channel which is populated by namespaces from Kubernetes API
// which match our labelSelector. It incapsulates logic required for creating a list of
// relevant namespaces.
func getNamespaces() chan *namespace {
	namespaces := make(chan *namespace)

	// asynchronously get namespaces via Kubernetes API
	// and coerce them to our custom 'namespace' type;
	// then push to the channel
	go func() {
		// always close channel before return
		// this signals to readers to stop listening
		// in case of error it'll be closed empty channel
		// in case of success it'll be channel populated by namespaces and closed when it's done
		defer func() {
			close(namespaces)
		}()

		log.Debug("Getting namespaces")

		nsList, err := k8sClient.CoreV1().Namespaces().List(metav1.ListOptions{LabelSelector: labelSelector})
		if err != nil {
			log.Error("Failed to get namespaces")
			log.Error(err)
			return
		}

		num := len(nsList.Items)

		log.Info(fmt.Sprintf("Found %d relevant namespaces", num))

		for _, ns := range nsList.Items {
			// get only those namespaces which are not in Terminating state currently
			if ns.Status.Phase != corev1.NamespaceTerminating {
				coercedNs := namespace(ns) // convert to our type to enable methods
				namespaces <- &coercedNs
			}
		}
	}()

	// immediately return a channel; it'll be eventually populated by goroutine above
	return namespaces
}

// filterWithDeletedBranches returns a channel of namespaces which have links to branches
// which respond 404
func filterWithDeletedBranches(namespaces chan *namespace) chan *namespace {
	out := make(chan *namespace)

	go func() {
		defer func() {
			close(out)
		}()

		var wg sync.WaitGroup

		for ns := range namespaces {
			// increment counter for WaitGroup
			wg.Add(1)
			// spawn goroutine for each namespace
			go func(ns *namespace) {
				defer func() {
					wg.Done() // decrement WaitGroup counter when function returns
				}()

				logger := ns.logger()

				logger.Debug("Checking branch")

				githubURL, err := ns.GithubSourceURL()
				if err != nil {
					logger.Error(err)
					return
				}

				// check Github Url
				status, err := getBranchURLStatus(githubURL)
				if err != nil {
					logger.Error(err)
					return
				}
				if status != 404 {
					logger.Info(fmt.Sprintf("Received status %d for URL %s, do nothing", status, githubURL))
					return
				}

				// it was 404, proceed
				logger.Info(fmt.Sprintf("Received status %d for URL %s, call the Terminator!", status, githubURL))
				out <- ns
			}(ns)
		}

		wg.Wait() // wait until WaitGroup counter equals zero
	}()

	return out
}

func filterHelmReleaseDeletedIfNeeded(namespaces chan *namespace) chan *namespace {
	out := make(chan *namespace)

	go func() {
		defer func() {
			close(out)
		}()

		var wg sync.WaitGroup

		for ns := range namespaces {
			// increment counter for WaitGroup
			wg.Add(1)
			// spawn goroutine for each namespace
			go func(ns *namespace) {
				defer func() {
					wg.Done() // decrement WaitGroup counter when function returns
				}()

				logger := ns.logger()

				logger.Debug("Deleting Helm release")

				retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					helmRelease, err := ns.HelmRelease()
					if err != nil {
						logger.Error(err)
						return nil // exit if there's no helm release defined for this namespace
					}

					logger.Info("Trying to delete Helm release")
					err = helm.DeleteRelease(helmRelease, k8sClient, k8sConfig)
					if err != nil {
						logger.Error(err)
						return err
					}
					logger.Info("Successfully deleted helm release")
					return nil
				})

				if retryErr != nil {
					logger.Error(retryErr)
					return
				}

				out <- ns
			}(ns)
		}

		wg.Wait() // wait until WaitGroup counter equals zero
	}()

	return out
}

func filterNamespaceDeleted(namespaces chan *namespace) chan *namespace {
	out := make(chan *namespace)

	go func() {
		defer func() {
			close(out)
		}()

		var wg sync.WaitGroup

		for ns := range namespaces {
			// increment counter for WaitGroup
			wg.Add(1)
			// spawn goroutine for each namespace
			go func(ns *namespace) {
				defer func() {
					wg.Done() // decrement WaitGroup counter when function returns
				}()

				logger := ns.logger()

				logger.Debug("Deleting namespace")

				// use "k8s.io/client-go/util/retry" package to retry on conflicts
				retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					logger.Debug("Getting namespace")
					k8sNs, err := k8sClient.CoreV1().Namespaces().Get(ns.Name(), metav1.GetOptions{})

					if err != nil {
						logger.Error(err)
						return nil // namespace not found, nothing to delete
					}

					if k8sNs.Status.Phase == corev1.NamespaceTerminating {
						logger.Warn("Namespace is in terminanting state, bailing out...")
						return nil
					}

					logger.Debug("Trying to delete namespace")
					err = k8sClient.CoreV1().Namespaces().Delete(ns.Name(), &metav1.DeleteOptions{})
					if err != nil {
						logger.Error(err)
						return err
					}
					logger.Info("Successfully deleted namespace")
					return nil
				})

				if retryErr != nil {
					logger.Error(retryErr)
					return
				}

				out <- ns
			}(ns)
		}

		wg.Wait() // wait until WaitGroup counter equals zero
	}()

	return out
}

// getBranchURLStatus expects URL like https://github.com/USER/REPO/tree/BRANCH
// it queries Github API and returns status code of HTTP response
func getBranchURLStatus(branchURL string) (status int, err error) {
	ghBranchURLRe := regexp.MustCompile("https://github.com/([^/]+)/([^/]+)/tree/([^/]+)")
	parts := ghBranchURLRe.FindStringSubmatch(branchURL)
	if parts == nil || len(parts) < 4 {
		return 0, fmt.Errorf("branchURL doesn't match regexp: %v", parts)
	}

	// get Github auth token from env variable and inject it into http client
	tokenSource := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv(ghTokenEnv)},
	)
	httpClient := oauth2.NewClient(context.Background(), tokenSource)

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches/%s", parts[1], parts[2], parts[3])

	resp, err := httpClient.Get(apiURL)
	defer resp.Body.Close()

	if err != nil {
		return 0, err
	}

	return resp.StatusCode, nil
}
