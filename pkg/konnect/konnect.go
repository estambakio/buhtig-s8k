package konnect

import (
	"flag"
	"os"

	"path/filepath"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/homedir"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// NewConfig returns K8s config
func NewConfig() (*rest.Config, error) {
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

	return config, nil
}

// NewClient returns new K8s client for given config (optional)
func NewClient(config *rest.Config) (client *kubernetes.Clientset, err error) {
	return kubernetes.NewForConfig(config)
}
