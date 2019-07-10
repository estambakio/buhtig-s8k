package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/homedir"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	log "github.com/sirupsen/logrus"

	helm "github.com/OpusCapita/buhtig-s8k/helm"
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
	clientset, config, err = k8sConnect()
	if err != nil {
		log.Fatal(err)
	}

	// assert if required env variables are defined
	assertEnv(ghUserEnv, ghTokenEnv)
}

func main() {
	for {
		log.Info("Getting namespaces")
		namespaces, err := clientset.CoreV1().Namespaces().List(metav1.ListOptions{LabelSelector: labelSelector})

		if err != nil {
			log.Warn(err.Error())
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
		time.Sleep(time.Minute) // TODO?: make if confugurable
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
		log.WithFields(log.Fields{"source": "getBranchURLStatus"}).Warn(err.Error())
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
		log.WithFields(log.Fields{"terminate": "failure", "namespace": name}).Warn(err.Error())
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
		log.WithFields(log.Fields{"delete-namespace": "failure", "namespace": name}).Warn("Failed to delete namespace")
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
		log.WithFields(log.Fields{"delete-helm-release": "failure", "name": helmRelease}).Warn("Failed to delete helm release")
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

	apiURL := &url.URL{
		Scheme: "https",
		Host:   "api.github.com",
		Path:   fmt.Sprintf("/repos/%s/%s/branches/%s", parts[1], parts[2], parts[3]),
	}

	log.Info(fmt.Sprintf("Going to request %s", apiURL.String()))
	req, err := http.NewRequest("GET", apiURL.String(), nil)
	if err != nil {
		return 0, err
	}

	// get Github credentials from environment and add them to request
	req.SetBasicAuth(os.Getenv(ghUserEnv), os.Getenv(ghTokenEnv))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

// assertEnv logs error messages if some env variables are not defined
func assertEnv(vars ...string) {
	log.Info("Asserting environment variables...")
	undef := []string{}
	for _, varName := range vars {
		if _, ok := os.LookupEnv(varName); !ok {
			undef = append(undef, varName)
		}
	}
	if len(undef) != 0 {
		log.Fatal(fmt.Sprintf("Env required but undefined: %s", strings.Join(undef, ", ")))
	}
	log.Info("Environment is fine")
}

// k8sConnect connects to Kubernetes cluster
func k8sConnect() (*kubernetes.Clientset, *rest.Config, error) {
	var err error
	var config *rest.Config

	if os.Getenv("APP_ENV") == "outside_cluster" {
		//outside-cluster config (for development)
		var kubeconfig *string

		if home := homedir.HomeDir(); home != "" {
			kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
		} else {
			kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
		}

		flag.Parse()

		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			return nil, nil, err
		}
	} else {
		// in-cluster config (production usage)
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, err
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	return clientset, config, err
}

func prettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	return string(s)
}
