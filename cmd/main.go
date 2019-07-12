package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	log "github.com/sirupsen/logrus"

	helm "github.com/OpusCapita/buhtig-s8k/pkg/helm"

	konnect "github.com/OpusCapita/buhtig-s8k/pkg/konnect"
)

const (
	labelSelector = "opuscapita.com/buhtig-s8k=true"

	githubURLAnnotationName   = "opuscapita.com/github-source-url"
	helmReleaseAnnotationName = "opuscapita.com/helm-release"

	ghUserEnv  = "GH_USER"
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
	assertEnv(ghUserEnv, ghTokenEnv)
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

			// start round trip
			log.Info("Getting namespaces")
			namespaces, err := clientset.CoreV1().Namespaces().List(metav1.ListOptions{LabelSelector: labelSelector})
			if err != nil {
				return err
			}

			log.Info(fmt.Sprintf("Found %d namespaces", len(namespaces.Items)))

			var wg sync.WaitGroup
			wg.Add(len(namespaces.Items))

			for _, ns := range namespaces.Items {
				go processNamespace(&ns, &wg)
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

	githubURL, ok := annotations[githubURLAnnotationName]
	if !ok {
		log.Warn(fmt.Sprintf("Annotation '%s' not set in namespace '%s'", githubURLAnnotationName, name))
		return
	}

	log.Info(fmt.Sprintf("Namespace '%s' has '%s' set to '%s'", name, githubURLAnnotationName, githubURL))

	// check Github Url
	status, err := getBranchURLStatus(githubURL)
	if err != nil {
		log.WithFields(log.Fields{"source": "getBranchURLStatus"}).Error(err)
		return
	}
	if status != 404 {
		log.Info(fmt.Sprintf("Received status %d for URL %s, do nothing", status, githubURL))
		return
	}
	log.Info(fmt.Sprintf("Received status %d for URL %s, call the Terminator!", status, githubURL))

	// delete namespace and Helm release
	err = terminate(ns)
	if err != nil {
		log.WithFields(log.Fields{"terminate": "failure", "namespace": name}).Error(err)
	} else {
		log.WithFields(log.Fields{"terminate": "success", "namespace": name}).Info("Namespace terminated successfully")
	}
}

// terminate is a function which deletes provided namespace
func terminate(ns *corev1.Namespace) error {
	name := ns.ObjectMeta.Name
	annotations := ns.ObjectMeta.Annotations

	// delete namespace
	err := clientset.CoreV1().Namespaces().Delete(name, &metav1.DeleteOptions{})
	if err != nil {
		log.WithFields(log.Fields{"delete-namespace": "failure", "namespace": name}).Error("Failed to delete namespace")
		return err
	}
	log.WithFields(log.Fields{"delete-namespace": "success", "namespace": name}).Info("Successfully deleted namespace")

	// lookup helm-release annotation
	helmRelease, ok := annotations[helmReleaseAnnotationName]
	if !ok {
		log.Warn(fmt.Sprintf("Annotation '%s' not set in namespace '%s', skip deleting Helm release", helmReleaseAnnotationName, name))
		return nil
	}

	// delete Helm release
	err = helm.DeleteHelmRelease(helmRelease, clientset, config)
	if err != nil {
		log.WithFields(log.Fields{"delete-helm-release": "failure", "name": helmRelease}).Error("Failed to delete helm release")
		return err
	}
	log.WithFields(log.Fields{"delete-helm-release": "success", "name": helmRelease}).Info("Successfully deleted helm release")
	return nil
}

// getBranchURLStatus expects URL like https://github.com/USER/REPO/tree/BRANCH
// it queries Github API and returns status code of HTTP response
func getBranchURLStatus(branchURL string) (status int, err error) {
	parts := ghBranchURLRe.FindStringSubmatch(branchURL)
	if parts == nil || len(parts) < 4 {
		return 0, fmt.Errorf("branchURL doesn't match regexp: %v", parts)
	}

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches/%s", parts[1], parts[2], parts[3])

	log.Info(fmt.Sprintf("Going to request %s", apiURL))
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0, err
	}

	// get Github credentials from environment and add them to request
	// credentials are required for querying private repositories
	// and give higher rate limits
	req.SetBasicAuth(os.Getenv(ghUserEnv), os.Getenv(ghTokenEnv))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}
