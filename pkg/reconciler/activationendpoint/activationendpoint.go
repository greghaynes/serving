/*
Copyright 2019 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package activationendpoint

import (
	"context"

	"k8s.io/client-go/tools/cache"

	commonlogging "github.com/knative/pkg/logging"
	networkingv1alpha1 "github.com/knative/serving/pkg/apis/networking/v1alpha1"
	networkinglisters "github.com/knative/serving/pkg/client/listers/networking/v1alpha1"
	rbase "github.com/knative/serving/pkg/reconciler"
)

const (
	reconcilerName = "ActivationEndpoints"
)

// reconciler implements controller.Reconciler for Service resources.
type reconciler struct {
	*rbase.Base

	aeLister networkinglisters.ActivationEndpointLister
}

// Reconcile compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the resource
// with the current status of the resource.
func (r *reconciler) Reconcile(ctx context.Context, key string) error {
	// Convert the namespace/name string into a distinct namespace and name
	_, _, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		r.Logger.Errorf("invalid resource key: %s", key)
		return err
	}
	logger := commonlogging.FromContext(ctx)
	logger.Info("Running reconcile ActivatorEndpoint")

	return nil
}

func (r *reconciler) reconcile(ctx context.Context, ae *networkingv1alpha1.ActivationEndpoint) error {
	return nil
}
