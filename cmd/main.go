package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
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

	k8sConfig, err = konnect.NewConfig()
	if err != nil {
		panic(err)
	}

	k8sClient, err = konnect.NewClient(k8sConfig)
	if err != nil {
		panic(err)
	}
}

func main() {
	start := make(chan struct{}, 1)
	errReport := make(chan error, 1)

	for {
		// spawn main routine
		go process(start, errReport)

		// start first iteration
		start <- struct{}{}

		// block until 'process' throws exception
		// and log returned error
		// then start over again
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

// process is the main function designed to run infinitely
func process(start chan struct{}, errReport chan<- error) {
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
			log.Info("Trigger received value -> Starting new iteration")

			log.Debug("Getting namespaces")

			nsList, err := k8sClient.CoreV1().Namespaces().List(metav1.ListOptions{LabelSelector: labelSelector})
			if err != nil {
				panic(err)
			}

			var namespaces []*namespace

			for _, ns := range nsList.Items {
				coercedNs := namespace(ns) // convert to our type to enable methods
				namespaces = append(namespaces, &coercedNs)
			}

			num := len(namespaces)

			log.Info(fmt.Sprintf("Found %d relevant namespaces", num))

			if num == 0 {
				go reschedule(start)
				continue
			}

			// make channel for namespaces which completed workflow doesn't matter how exactly
			complete := make(chan *namespace, num)

			// process all namespaces in parallel
			// we can also limit number of parallel executions if needed, using other technics
			for _, ns := range namespaces {
				go processNamespace(ns, complete)
			}

			waitFor := num

			// this loop will exit when 'complete' channel is closed
			for ns := range complete {
				ns.logger().Debug("Namespace completed")
				waitFor--
				if waitFor == 0 {
					// this for loop ('range complete') will exit when 'complete' channel is closed
					close(complete)

					log.Debug("All namespaces completed, time to reschedule")
					go reschedule(start)
				}
			}
		}
	}
}

func reschedule(start chan<- struct{}) {
	log.Info("Sleep")
	<-time.After(time.Minute)
	log.Debug("Reschedule")
	start <- struct{}{}
}

// processNamespace does a single run of our business logic for particular namespace
func processNamespace(ns *namespace, done chan<- *namespace) {
	// deferred function is called right before function returns
	// we don't care what the outcome was, we just need to know when this run is finished
	defer func() {
		done <- ns
	}()

	logger := ns.logger()

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

	// delete Helm release
	if err := deleteHelmReleaseForNamespace(ns); err != nil {
		logger.Error(err)
		return
	}

	// if Helm release was successfully deleted then delete namespace
	err = deleteNamespace(ns)
	if err != nil {
		logger.Error(err)
	} else {
		logger.Info("Termination succeeded")
	}
}

// deleteNamespace is a function which deletes provided namespace
func deleteNamespace(ns *namespace) error {
	logger := ns.logger()

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

		logger.Info("Trying to delete namespace")
		err = k8sClient.CoreV1().Namespaces().Delete(ns.Name(), &metav1.DeleteOptions{})
		if err != nil {
			logger.Error(err)
			return err
		}
		logger.Info("Successfully deleted namespace")
		return nil
	})

	return retryErr
}

// deleteHelmReleaseForNamespace is a function which deletes Helm release associated with this namespace
func deleteHelmReleaseForNamespace(ns *namespace) error {
	logger := ns.logger()

	// use "k8s.io/client-go/util/retry" package to retry on conflicts
	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		helmRelease, err := ns.HelmRelease()
		if err != nil {
			return err
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

	return retryErr
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
