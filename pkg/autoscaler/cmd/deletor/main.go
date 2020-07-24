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
	"log"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
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

func setupLogger() *zap.SugaredLogger {
	config, err := logging.NewConfigFromMap(nil)
	if err != nil {
		log.Fatalf("Failed to create logging config: %s", err)
	}

	logger, _ := logging.NewLoggerFromConfig(config, "autoscaler-migration")
	return logger
}
