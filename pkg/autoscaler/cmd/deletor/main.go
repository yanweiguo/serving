/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"fmt"
	"io"
	"log"
	"time"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog"
	"k8s.io/legacy-cloud-providers/gce"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
)

const (
	// Sleep interval to retry cloud client creation.
	cloudClientRetryInterval = 10 * time.Second
)

func main() {
	logger := setupLogger()
	defer logger.Sync()

	kubeClient, err := kubeClient()
	if err != nil {
		panic(err)
	}

	as, err := kubeClient.AppsV1().Deployments(system.Namespace()).Get("autoscaler", metav1.GetOptions{})
	if err != nil {
		logger.Error("Failed to find deployment autoscaler")
	}

	gceClient()

	logger.Infof("%v", as.Labels)
	logger.Info("Migration complete")
}

func kubeClient() (*kubernetes.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		return nil, fmt.Errorf("error building kubeconfig: %w", err)
	}
	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error building kube clientset: %w", err)
	}
	return kubeClient, nil
}

func gceClient() *gce.Cloud {
	configReader := func() io.Reader { return nil }

	// Creating the cloud interface involves resolving the metadata server to get
	// an oauth token. If this fails, the token provider assumes it's not on GCE.
	// No errors are thrown. So we need to keep retrying till it works because
	// we know we're on GCE.
	for {
		provider, err := cloudprovider.GetCloudProvider("gce", configReader())
		if err == nil {
			cloud := provider.(*gce.Cloud)
			// If this controller is scheduled on a node without compute/rw
			// it won't be allowed to list backends. We can assume that the
			// user has no need for Ingress in this case. If they grant
			// permissions to the node they will have to restart the controller
			// manually to re-create the client.
			if bss, err := cloud.ListGlobalBackendServices(); err == nil {
				for _, bs := range bss {
					fmt.Printf("bs name: %s\n", bs.Name)
				}
				return cloud
			}
			klog.Warningf("Failed to list backend services, retrying: %v", err)
		} else {
			klog.Warningf("Failed to get cloud provider, retrying: %v", err)
		}
		time.Sleep(cloudClientRetryInterval)
	}
}

func setupLogger() *zap.SugaredLogger {
	config, err := logging.NewConfigFromMap(nil)
	if err != nil {
		log.Fatalf("Failed to create logging config: %s", err)
	}

	logger, _ := logging.NewLoggerFromConfig(config, "autoscaler-migration")
	return logger
}
