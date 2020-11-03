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

// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
	networkingv1alpha1 "knative.dev/networking/pkg/apis/networking/v1alpha1"
	versioned "knative.dev/networking/pkg/client/clientset/versioned"
	internalinterfaces "knative.dev/networking/pkg/client/informers/externalversions/internalinterfaces"
	v1alpha1 "knative.dev/networking/pkg/client/listers/networking/v1alpha1"
)

// ClusterDomainClaimInformer provides access to a shared informer and lister for
// ClusterDomainClaims.
type ClusterDomainClaimInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.ClusterDomainClaimLister
}

type clusterDomainClaimInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
}

// NewClusterDomainClaimInformer constructs a new informer for ClusterDomainClaim type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewClusterDomainClaimInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredClusterDomainClaimInformer(client, resyncPeriod, indexers, nil)
}

// NewFilteredClusterDomainClaimInformer constructs a new informer for ClusterDomainClaim type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredClusterDomainClaimInformer(client versioned.Interface, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.NetworkingV1alpha1().ClusterDomainClaims().List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.NetworkingV1alpha1().ClusterDomainClaims().Watch(context.TODO(), options)
			},
		},
		&networkingv1alpha1.ClusterDomainClaim{},
		resyncPeriod,
		indexers,
	)
}

func (f *clusterDomainClaimInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredClusterDomainClaimInformer(client, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *clusterDomainClaimInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&networkingv1alpha1.ClusterDomainClaim{}, f.defaultInformer)
}

func (f *clusterDomainClaimInformer) Lister() v1alpha1.ClusterDomainClaimLister {
	return v1alpha1.NewClusterDomainClaimLister(f.Informer().GetIndexer())
}
