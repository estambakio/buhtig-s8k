package helm

import (
	"fmt"
	"os"

	"k8s.io/helm/pkg/helm"
	"k8s.io/helm/pkg/helm/environment"
	"k8s.io/helm/pkg/helm/portforwarder"
	"k8s.io/helm/pkg/kube"
	"k8s.io/helm/pkg/proto/hapi/release"

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

// DeleteRelease deletes provided Helm release
// we need to port-forward to get access to Tiller server. Port-forwarding logic is taken from helm lib.
func DeleteRelease(name string, client kubernetes.Interface, config *rest.Config) error {
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
		logger.Debug(fmt.Sprintf("Created tunnel using local port: '%d'\n", tillerTunnel.Local))
	}

	// Set up the gRPC config.
	logger.Debug(fmt.Sprintf("SERVER: %q\n", settings.TillerHost))

	defer func() {
		if tillerTunnel != nil {
			tillerTunnel.Close()
		}
	}()

	options := []helm.Option{helm.Host(settings.TillerHost), helm.ConnectTimeout(settings.TillerConnectionTimeout)}

	// create Helm client finally
	helmClient := helm.NewClient(options...)

	// fail quickly if tiller doesn't respond (maybe will provide more useful errors in this case)
	if err := helmClient.PingTiller(); err != nil {
		return err
	}

	logger.Debug("Check if release exists")
	rs, err := helmClient.ReleaseStatus(name)
	if err != nil {
		logger.Error(err)
		return nil
	}
	statusCode := rs.GetInfo().GetStatus().GetCode()
	log.Debug(fmt.Sprintf("Release status: %d", statusCode))
	if statusCode == release.Status_DELETED || statusCode == release.Status_DELETING {
		logger.Debug(fmt.Sprintf("Helm release status = %v, skip trying to delete", statusCode))
		return nil
	}

	logger.Info("Deleting Helm release")
	resp, err := helmClient.DeleteRelease(name, helm.DeletePurge(true) /*, helm.DeleteTimeout(60)*/)
	if err != nil {
		logger.Error(err)
		return err
	}

	// log text response from delete request
	log.WithFields(log.Fields{"source": "helm"}).Debug(resp.Info)

	return nil
}
