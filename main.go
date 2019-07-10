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
)

const (
	labelSelector = "opuscapita.com/buhtig-s8k=true"

	githubUrlAnnotationName   = "opuscapita.com/github-source-url"
	helmReleaseAnnotationName = "opuscapita.com/helm-release"

	ghUserEnv  = "GH_USER"
	ghTokenEnv = "GH_TOKEN"
)

var clientset *kubernetes.Clientset
var ghBranchUrlRe *regexp.Regexp

func init() {
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	ghBranchUrlRe = regexp.MustCompile("https://github.com/([^/]+)/([^/]+)/tree/([^/]+)")

	// setup kubernetes client
	var err error
	clientset, err = k8sConnect()
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
	// wg.Done() is called before function returns
	// we don't care what the outcome was, we just need to know when the run is done
	defer wg.Done()

	name := ns.ObjectMeta.Name
	annotations := ns.ObjectMeta.Annotations

	githubUrl, ok := annotations[githubUrlAnnotationName]
	if !ok {
		log.Warn(fmt.Sprintf("Annotation '%s' not set in namespace '%s'", githubUrlAnnotationName, name))
		return
	}

	log.Info(fmt.Sprintf("Namespace '%s' has '%s' set to '%s'", name, githubUrlAnnotationName, githubUrl))

	// check Github Url
	status, err := getBranchUrlStatus(githubUrl)
	if err != nil {
		log.WithFields(log.Fields{"source": "getBranchUrlStatus"}).Warn(err.Error())
		return
	}
	if status != 404 {
		log.Info(fmt.Sprintf("Received status %d for URL %s, do nothing", status, githubUrl))
		return
	}
	log.Info(fmt.Sprintf("Received status %d for URL %s, call the Terminator!", status, githubUrl))

	err = terminate(ns, clientset)
	if err != nil {
		log.WithFields(log.Fields{"terminate": "failure", "namespace": name}).Warn(err.Error())
	} else {
		log.WithFields(log.Fields{"terminate": "success", "namespace": name}).Info("Namespace deleted successfully")
	}
}

// terminate is a function which deletes provided namespace
func terminate(ns *corev1.Namespace, clientset *kubernetes.Clientset) error {
	name := ns.ObjectMeta.Name
	// annotations := ns.ObjectMeta.Annotations
	// TODO delete Helm release if needed

	// delete namespace
	err := clientset.CoreV1().Namespaces().Delete(name, &metav1.DeleteOptions{})
	return err
}

// getBranchUrlStatus expects URL like https://github.com/USER/REPO/tree/BRANCH
// it queries Github API and returns status code of HTTP response
func getBranchUrlStatus(branchUrl string) (status int, err error) {
	parts := ghBranchUrlRe.FindStringSubmatch(branchUrl)
	if parts == nil || len(parts) < 4 {
		return 0, fmt.Errorf("branchUrl doesn't match regexp: %v", parts)
	}

	apiUrl := &url.URL{
		Scheme: "https",
		Host:   "api.github.com",
		Path:   fmt.Sprintf("/repos/%s/%s/branches/%s", parts[1], parts[2], parts[3]),
	}

	log.Info(fmt.Sprintf("Going to request %s", apiUrl.String()))
	req, err := http.NewRequest("GET", apiUrl.String(), nil)
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
func k8sConnect() (*kubernetes.Clientset, error) {
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
			return nil, err
		}
	} else {
		// in-cluster config (production usage)
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}

	return kubernetes.NewForConfig(config)
}

func prettyPrint(i interface{}) string {
	s, _ := json.MarshalIndent(i, "", "\t")
	return string(s)
}
