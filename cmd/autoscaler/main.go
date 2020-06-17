/*
Copyright 2018 The Knative Authors

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

// Multitenant autoscaler executable.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	basecmd "github.com/kubernetes-incubator/custom-metrics-apiserver/pkg/cmd"
	"github.com/spf13/pflag"
	"go.opencensus.io/stats/view"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"k8s.io/apimachinery/pkg/util/wait"
	corev1informers "k8s.io/client-go/informers/core/v1"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"

	kubeclient "knative.dev/pkg/client/injection/kube/client"
	endpointsinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/endpoints"
	podinformer "knative.dev/pkg/client/injection/kube/informers/core/v1/pod"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/injection/sharedmain"
	pkgnet "knative.dev/pkg/network"
	"knative.dev/pkg/websocket"

	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	kle "knative.dev/pkg/leaderelection"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/metrics"
	"knative.dev/pkg/profiling"
	"knative.dev/pkg/signals"
	"knative.dev/pkg/system"
	"knative.dev/pkg/version"
	av1alpha1 "knative.dev/serving/pkg/apis/autoscaling/v1alpha1"
	"knative.dev/serving/pkg/apis/serving"
	asmetrics "knative.dev/serving/pkg/autoscaler/metrics"
	"knative.dev/serving/pkg/autoscaler/scaling"
	"knative.dev/serving/pkg/autoscaler/statserver"
	smetrics "knative.dev/serving/pkg/metrics"
	"knative.dev/serving/pkg/reconciler/autoscaling/kpa"
	"knative.dev/serving/pkg/reconciler/metric"
	"knative.dev/serving/pkg/resources"
)

const (
	statsServerAddr = ":8080"
	statsBufferLen  = 1000
	component       = "autoscaler"
	controllerNum   = 2
)

var (
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
)

func main() {
	// Initialize early to get access to flags and merge them with the autoscaler flags.
	customMetricsAdapter := &basecmd.AdapterBase{}
	customMetricsAdapter.Flags().AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	// Set up signals so we handle the first shutdown signal gracefully.
	ctx := signals.NewContext()

	// Report stats on Go memory usage every 30 seconds.
	msp := metrics.NewMemStatsAll()
	msp.Start(ctx, 30*time.Second)
	if err := view.Register(msp.DefaultViews()...); err != nil {
		log.Fatal("Error exporting go memstats view: ", err)
	}

	cfg, err := sharedmain.GetConfig(*masterURL, *kubeconfig)
	if err != nil {
		log.Fatal("Error building kubeconfig: ", err)
	}

	log.Printf("Registering %d clients", len(injection.Default.GetClients()))
	log.Printf("Registering %d informer factories", len(injection.Default.GetInformerFactories()))
	log.Printf("Registering %d informers", len(injection.Default.GetInformers()))
	log.Printf("Registering %d controllers", controllerNum)

	// Adjust our client's rate limits based on the number of controller's we are running.
	cfg.QPS = controllerNum * rest.DefaultQPS
	cfg.Burst = controllerNum * rest.DefaultBurst

	ctx, informers := injection.Default.SetupInformers(ctx, cfg)

	kubeClient := kubeclient.Get(ctx)

	// We sometimes startup faster than we can reach kube-api. Poll on failure to prevent us terminating
	if perr := wait.PollImmediate(time.Second, 60*time.Second, func() (bool, error) {
		if err = version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
			log.Print("Failed to get k8s version ", err)
		}
		return err == nil, nil
	}); perr != nil {
		log.Fatal("Timed out attempting to get k8s version: ", err)
	}

	// Set up our logger.
	loggingConfig, err := sharedmain.GetLoggingConfig(ctx)
	if err != nil {
		log.Fatal("Error loading/parsing logging configuration: ", err)
	}
	logger, atomicLevel := logging.NewLoggerFromConfig(loggingConfig, component)
	defer flush(logger)
	ctx = logging.WithLogger(ctx, logger)

	// Set up leader election config
	leaderElectionConfig, err := sharedmain.GetLeaderElectionConfig(ctx)
	if err != nil {
		logger.Fatalf("Error loading leader election configuration: %v", err)
	}
	leConfig := leaderElectionConfig.GetComponentConfig(component)
	if leConfig.LeaderElect {
		// Signal that we are executing in a context with leader election.
		ctx = kle.WithStatefulSetLeaderElectorBuilder(ctx, kubeclient.Get(ctx), leConfig)
	}

	// statsCh is the main communication channel between the stats server and multiscaler.
	statsCh := make(chan asmetrics.StatMessage, statsBufferLen)
	defer close(statsCh)

	profilingHandler := profiling.NewHandler(logger, false)

	cmw := configmap.NewInformedWatcher(kubeclient.Get(ctx), system.Namespace())
	// Watch the logging config map and dynamically update logging levels.
	cmw.Watch(logging.ConfigMapName(), logging.UpdateLevelFromConfigMap(logger, atomicLevel, component))
	// Watch the observability config map
	cmw.Watch(metrics.ConfigMapName(),
		metrics.ConfigMapWatcher(component, nil /* SecretFetcher */, logger),
		profilingHandler.UpdateFromConfigMap)

	endpointsInformer := endpointsinformer.Get(ctx)
	podInformer := podinformer.Get(ctx)

	collector := asmetrics.NewMetricCollector(
		statsScraperFactoryFunc(endpointsInformer.Lister(), podInformer.Lister()), logger)
	customMetricsAdapter.WithCustomMetrics(asmetrics.NewMetricProvider(collector))

	// Set up scalers.
	// uniScalerFactory depends endpointsInformer to be set.
	multiScaler := scaling.NewMultiScaler(ctx.Done(), uniScalerFactoryFunc(endpointsInformer, collector), logger)

	controllers := []*controller.Impl{
		kpa.NewController(ctx, cmw, multiScaler),
		metric.NewController(ctx, cmw, collector),
	}

	// Set up a statserver.
	statsServer := statserver.New(statsServerAddr, statsCh, logger)

	// Start watching the configs.
	if err := cmw.Start(ctx.Done()); err != nil {
		logger.Fatalw("Failed to start watching configs", zap.Error(err))
	}

	// Start all of the informers and wait for them to sync.
	if err := controller.StartInformers(ctx.Done(), informers...); err != nil {
		logger.Fatalw("Failed to start informers", zap.Error(err))
	}

	go controller.StartAll(ctx, controllers...)

	ases := []*websocket.ManagedConnection{}
	ordinal, _ := extraOrdinalFromPodName()
	io := int(ordinal)
	// Open a WebSocket connection to the autoscaler.
	for i := 0; i < 3; i++ {
		if i != io {
			autoscalerEndpoint := fmt.Sprintf("ws://autoscaler-%d.autoscaler.%s.svc.%s:8080", i, system.Namespace(), pkgnet.GetClusterDomainName())
			logger.Info("Connecting to Autoscaler at ", autoscalerEndpoint)
			ases = append(ases, websocket.NewDurableSendingConnection(autoscalerEndpoint, logger))
		}
	}
	go func() {
		for sm := range statsCh {
			collector.Record(sm.Key, sm.Stat)
			multiScaler.Poke(sm.Key, sm.Stat)
			sm.Stat.IsFromActivator = false
			for _, as := range ases {
				as.Send(sm)
			}
		}
	}()

	profilingServer := profiling.NewServer(profilingHandler)

	eg, egCtx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return customMetricsAdapter.Run(ctx.Done())
	})
	eg.Go(statsServer.ListenAndServe)
	eg.Go(profilingServer.ListenAndServe)

	// This will block until either a signal arrives or one of the grouped functions
	// returns an error.
	<-egCtx.Done()

	statsServer.Shutdown(5 * time.Second)
	profilingServer.Shutdown(context.Background())
	for _, as := range ases {
		as.Shutdown()
	}
	// Don't forward ErrServerClosed as that indicates we're already shutting down.
	if err := eg.Wait(); err != nil && err != http.ErrServerClosed {
		logger.Errorw("Error while running server", zap.Error(err))
	}
}

func extraOrdinalFromPodName() (uint64, error) {
	n := os.Getenv("POD_NAME")
	if i := strings.LastIndex(n, "-"); i != -1 {
		return strconv.ParseUint(n[i+1:], 10, 64)
	}

	return 0, fmt.Errorf("ordinal not found from name %s", n)
}

func uniScalerFactoryFunc(endpointsInformer corev1informers.EndpointsInformer,
	metricClient asmetrics.MetricClient) scaling.UniScalerFactory {
	return func(decider *scaling.Decider) (scaling.UniScaler, error) {
		if v, ok := decider.Labels[serving.ConfigurationLabelKey]; !ok || v == "" {
			return nil, fmt.Errorf("label %q not found or empty in Decider %s", serving.ConfigurationLabelKey, decider.Name)
		}
		if decider.Spec.ServiceName == "" {
			return nil, fmt.Errorf("%s decider has empty ServiceName", decider.Name)
		}

		serviceName := decider.Labels[serving.ServiceLabelKey] // This can be empty.
		configName := decider.Labels[serving.ConfigurationLabelKey]

		// Create a stats reporter which tags statistics by PA namespace, configuration name, and PA name.
		ctx, err := smetrics.RevisionContext(decider.Namespace, serviceName, configName, decider.Name)
		if err != nil {
			return nil, err
		}

		return scaling.New(decider.Namespace, decider.Name, metricClient, endpointsInformer.Lister(), &decider.Spec, ctx)
	}
}

func statsScraperFactoryFunc(endpointsLister corev1listers.EndpointsLister,
	podLister corev1listers.PodLister) asmetrics.StatsScraperFactory {
	return func(metric *av1alpha1.Metric, logger *zap.SugaredLogger) (asmetrics.StatsScraper, error) {
		podCounter := resources.NewScopedEndpointsCounter(
			endpointsLister, metric.Namespace, metric.Spec.ScrapeTarget)
		// TODO(vagababov): while metric name == revision name, we should utilize the proper
		// values from the labels.
		podAccessor := resources.NewPodAccessor(podLister, metric.Namespace, metric.Name)
		return asmetrics.NewStatsScraper(metric, podCounter, podAccessor, logger)
	}
}

func flush(logger *zap.SugaredLogger) {
	logger.Sync()
	metrics.FlushExporter()
}
