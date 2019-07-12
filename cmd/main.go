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

	"k8s.io/client-go/kubernetes"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

var clientset *kubernetes.Clientset
var config *rest.Config
var ghBranchURLRe *regexp.Regexp

func init() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	ghBranchURLRe = regexp.MustCompile("https://github.com/([^/]+)/([^/]+)/tree/([^/]+)")

	// setup kubernetes client
	var err error
	config, err = konnect.NewConfig()
	if err != nil {
		log.Fatal(err)
	}

	clientset, err = konnect.NewClientset(config)
	if err != nil {
		log.Fatal(err)
	}

	// assert if required env variables are defined
	assertEnv(ghTokenEnv)
}

func main() {
	for {
		// run every iteration in a function to make use of panic recovery in order to
		// avoid crushing if panic happens somewhere downstream
		err := func() error {
			var err error

			// recover from panic in case it happens somewhere downstream
			// and set appropriate err value
			defer func() {
				if r := recover(); r != nil {
					switch t := r.(type) {
					case string:
						err = errors.New(t)
					case error:
						err = t
					default:
						err = errors.New(fmt.Sprintf("%v", t))
					}
				}
			}()

			log.Info("Getting namespaces")

			namespaces, err := clientset.CoreV1().Namespaces().List(metav1.ListOptions{LabelSelector: labelSelector})
			if err != nil {
				return err
			}

			log.Info(fmt.Sprintf("Found %d namespaces", len(namespaces.Items)))

			var wg sync.WaitGroup
			wg.Add(len(namespaces.Items))

			for _, ns := range namespaces.Items {
				go func(ns corev1.Namespace) {
					processNamespace(&ns, &wg) // TODO: consider using channels instead of WG and probably select on functions and common time.Timer timeout
				}(ns) // make local variable; otherwise all goroutines will get the latest one, like in JavaScript's setTimeout situation
			}

			wg.Wait() // blocks until wg.Done() is called len(namespaces.Items) times

			// err == nil
			// it can be != nil only if panic has happened;
			// in case of panic 'defer' function sets err value
			return err
		}()

		if err != nil {
			// exception was catched
			log.WithFields(log.Fields{"iteration": "error"}).Error(err)
		}

		log.Info("Sleeping")
		time.Sleep(time.Minute) // TODO?: make it configurable
	}
}

// processNamespace does a single run of our business logic for particular namespace
func processNamespace(ns *corev1.Namespace, wg *sync.WaitGroup) {
	// wg.Done() is called right before function returns
	// we don't care what the outcome was, we just need to know when this run is finished
	defer wg.Done()

	name := ns.ObjectMeta.Name
	annotations := ns.ObjectMeta.Annotations

	logger := log.WithFields(log.Fields{"namespace": name, "func": "processNamespace"})

	githubURL, ok := annotations[githubURLAnnotationName]
	if !ok {
		logger.Warn(fmt.Sprintf("Annotation '%s' not set", githubURLAnnotationName))
		return
	}

	logger.Info(fmt.Sprintf("%s = %s", githubURLAnnotationName, githubURL))

	// check Github Url
	status, err := getBranchURLStatus(githubURL)
	if err != nil {
		log.WithFields(log.Fields{"source": "getBranchURLStatus"}).Error(err)
		return
	}
	if status != 404 {
		logger.Info(fmt.Sprintf("Received status %d for URL %s, do nothing", status, githubURL))
		return
	}
	logger.Info(fmt.Sprintf("Received status %d for URL %s, call the Terminator!", status, githubURL))

	if err := deleteHelmReleaseForNamespace(name); err != nil {
		logger.Error(err)
		return
	}

	err = deleteNamespace(name)
	if err != nil {
		logger.Error(err)
	} else {
		logger.Info("Namespace terminated successfully")
	}
}

// deleteNamespace is a function which deletes provided namespace
func deleteNamespace(name string) error {
	logger := log.WithFields(log.Fields{"namespace": name, "func": "deleteNamespace"})

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		logger.Info("Getting namespace")
		ns, err := clientset.CoreV1().Namespaces().Get(name, metav1.GetOptions{})

		if err != nil {
			logger.Error(err)
			return nil // namespace not found, nothing to delete
		}

		if ns.Status.Phase == corev1.NamespaceTerminating {
			logger.Warn("Namespace is in terminanting state, bailing out...")
			return nil
		}

		logger.Info("Trying to delete namespace")
		err = clientset.CoreV1().Namespaces().Delete(name, &metav1.DeleteOptions{})
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
func deleteHelmReleaseForNamespace(name string) error {
	logger := log.WithFields(log.Fields{"namespace": name, "func": "deleteHelmRelease"})

	retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		logger.Info("Getting namespace")
		ns, err := clientset.CoreV1().Namespaces().Get(name, metav1.GetOptions{})
		if err != nil {
			logger.Error(err)
			return nil // namespace not found, nothing to delete
		}

		// lookup helm-release annotation
		helmReleaseName, ok := ns.ObjectMeta.Annotations[helmReleaseAnnotationName]
		if !ok {
			logger.Warn(fmt.Sprintf("Annotation '%s' not set in namespace '%s', skip deleting Helm release", helmReleaseAnnotationName, name))
			return nil
		}

		logger.Info("Trying to delete Helm release")
		err = helm.DeleteRelease(helmReleaseName, clientset, config)
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
	parts := ghBranchURLRe.FindStringSubmatch(branchURL)
	if parts == nil || len(parts) < 4 {
		return 0, fmt.Errorf("branchURL doesn't match regexp: %v", parts)
	}

	// get auth token from env variable
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
