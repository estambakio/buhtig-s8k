package helm

import (
	"fmt"
	"os"

	"k8s.io/helm/pkg/helm"
	"k8s.io/helm/pkg/helm/environment"
	"k8s.io/helm/pkg/helm/portforwarder"
	"k8s.io/helm/pkg/kube"

	log "github.com/sirupsen/logrus"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"k8s.io/client-go/util/homedir"
	"k8s.io/helm/pkg/helm/helmpath"
)

const (
	tillerNamespaceEnv = "TILLER_NAMESPACE"
)

var (
	tillerTunnel *kube.Tunnel
	settings     environment.EnvSettings
)

// DeleteRelease deletes provided release
func DeleteRelease(name string, client *kubernetes.Clientset, config *rest.Config) error {
	logger := log.WithFields(log.Fields{"helm-release": name, "func": "helm.DeleteRelease"})

	if tns, ok := os.LookupEnv(tillerNamespaceEnv); ok {
		settings.TillerNamespace = tns
	} else {
		settings.TillerNamespace = "kube-system"
	}

	settings.Home = helmpath.Home(homedir.HomeDir() + "/.helm")
	settings.TillerConnectionTimeout = 60

	if settings.TillerHost == "" {
		tillerTunnel, err := portforwarder.New(settings.TillerNamespace, client, config)
		if err != nil {
			return err
		}

		settings.TillerHost = fmt.Sprintf("127.0.0.1:%d", tillerTunnel.Local)
		logger.Info(fmt.Sprintf("Created tunnel using local port: '%d'\n", tillerTunnel.Local))
	}

	// Set up the gRPC config.
	logger.Info(fmt.Sprintf("SERVER: %q\n", settings.TillerHost))

	defer func() {
		if tillerTunnel != nil {
			tillerTunnel.Close()
		}
	}()

	options := []helm.Option{helm.Host(settings.TillerHost), helm.ConnectTimeout(settings.TillerConnectionTimeout)}

	helmClient := helm.NewClient(options...)

	if err := helmClient.PingTiller(); err != nil {
		return err
	}

	logger.Info("Check if release exists")
	if _, err := helmClient.ReleaseStatus(name); err != nil {
		logger.Error(err)
		return nil
	}

	logger.Info("Deleting Helm release")
	resp, err := helmClient.DeleteRelease(name, helm.DeletePurge(true), helm.DeleteTimeout(10))
	if err != nil {
		logger.Error(err)
		return err
	}

	log.WithFields(log.Fields{"source": "helm"}).Info(resp.Info)

	return nil
}
