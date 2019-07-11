package main

import (
	"context"
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
		log.Info("Getting namespaces")
		namespaces, err := clientset.CoreV1().Namespaces().List(metav1.ListOptions{LabelSelector: labelSelector})

		if err != nil {
			log.Error(err)
			continue
		}

		log.Info(fmt.Sprintf("Found %d namespaces", len(namespaces.Items)))

		var wg sync.WaitGroup
		wg.Add(len(namespaces.Items))

		for _, ns := range namespaces.Items {
			go processNamespace(&ns, &wg)
		}

		wg.Wait() // blocks until wg.Done() is called len(namespaces.Items) times

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
	err = helm.DeleteRelease(helmRelease, clientset, config)
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
