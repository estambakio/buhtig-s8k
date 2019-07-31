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

func main() {
	log.SetLevel(log.DebugLevel)
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	// assert if required env variables are defined
	assertEnv(ghTokenEnv)

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

					// main logic happens here
					// make a channel of namespaces and filter it sequentially
					// filter functions actually do some work: delete Helm release, delete namespace, etc.
					// every step returns a channel which is populated in a separate goroutine
					// therefore all namespaces are processed concurrently
					// items in the resulting channel are those namespaces which completed all consequent steps in workflow
					// (e.g. returned 'true' for all predicates one after another)
					terminated := getNamespaces(k8sClient).
						filter(isBranchDeleted).
						filter(isHelmReleaseDeletedIfNeeded(k8sClient, k8sConfig)).
						filter(isNamespaceDeleted(k8sClient))

					// this loop blocks until 'terminated' channel is closed
					for ns := range terminated {
						ns.logger().Debug("Completely terminated")
					}

					log.Debug("All namespaces processed, time to reschedule")
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
// TODO: find out if there's better, more obvious way to do such things
type namespace corev1.Namespace

// newNamespace converts K8s namespace to our 'namespace' type
func newNamespace(k8sNs corev1.Namespace) *namespace {
	coercedNs := namespace(k8sNs)
	return &coercedNs
}

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

// nsChan is a type for channel of namespaces
type nsChan chan *namespace

// filter takes nsChan as input and produces nsChan as output where
// all elements matched predicate function
// see https://blog.golang.org/pipelines (fan-in, fan-out) for details about this pattern
func (in nsChan) filter(predicate func(*namespace) bool) nsChan {
	out := make(nsChan)

	go func() {
		// always close channel before return
		// this signals to readers to stop listening
		defer func() {
			close(out)
		}()

		var wg sync.WaitGroup

		for ns := range in {
			// increment counter for WaitGroup
			wg.Add(1)
			// spawn goroutine for each namespace
			go func(ns *namespace) {
				defer func() {
					wg.Done() // decrement WaitGroup counter when function returns
				}()

				// if predicate returns true then push to output channel
				if predicate(ns) {
					out <- ns
				}
			}(ns)
		}

		// wait until WaitGroup counter equals zero before returning
		// it unblocks when all elements are processed by inner goroutines
		// and we can safely close output channel (using deferred function in this case)
		wg.Wait()
	}()

	// immediately return a channel; it'll be eventually populated by goroutine above
	return out
}

// getNamespaces returns a channel which is populated by namespaces from Kubernetes API
// which match our labelSelector. It incapsulates logic required for creating a list of
// relevant namespaces.
func getNamespaces(k8sClient kubernetes.Interface) nsChan {
	namespaces := make(nsChan)

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

		timeout := int64(20) // seconds
		listOptions := metav1.ListOptions{
			LabelSelector:  labelSelector,
			TimeoutSeconds: &timeout,
		}
		nsList, err := k8sClient.CoreV1().Namespaces().List(listOptions)
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
				namespaces <- newNamespace(ns)
			}
		}
	}()

	// immediately return a channel; it'll be eventually populated by goroutine above
	return namespaces
}

func isBranchDeleted(ns *namespace) bool {
	logger := ns.logger()

	logger.Debug("Checking branch")

	githubURL, err := ns.GithubSourceURL()
	if err != nil {
		logger.Error(err)
		return false
	}

	// check Github Url
	status, err := getBranchURLStatus(githubURL)
	if err != nil {
		logger.Error(err)
		return false
	}
	if status != 404 {
		logger.Info(fmt.Sprintf("Received status %d for URL %s, do nothing", status, githubURL))
		return false
	}

	// it was 404, proceed
	logger.Info(fmt.Sprintf("Received status %d for URL %s, call the Terminator!", status, githubURL))
	return true
}

func isHelmReleaseDeletedIfNeeded(k8sClient kubernetes.Interface, k8sConfig *rest.Config) func(*namespace) bool {
	return func(ns *namespace) bool {
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
			return false
		}

		return true
	}
}

// isNamespaceDeleted deletes namespace from Kubernetes if it exists
// returns false if namespace deletion fails, true otherwise
func isNamespaceDeleted(k8sClient kubernetes.Interface) func(*namespace) bool {
	return func(ns *namespace) bool {
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
			return false
		}

		return true
	}
}

// getBranchURLStatus expects URL like https://github.com/USER/REPO/tree/BRANCH
// it queries Github API and returns status code of HTTP response
func getBranchURLStatus(branchURL string) (status int, err error) {
	ghBranchURLRe := regexp.MustCompile("https://github.com/([^/]+)/([^/]+)/tree/(.+)")
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
