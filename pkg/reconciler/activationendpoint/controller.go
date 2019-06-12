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
	"github.com/knative/pkg/controller"
	networkinginformers "github.com/knative/serving/pkg/client/informers/externalversions/networking/v1alpha1"
	rbase "github.com/knative/serving/pkg/reconciler"
)

const (
	controllerAgentName = "activationendpoints-controller"
)

// NewController initializes the controller and is called by the generated code.
// Registers eventhandlers to enqueue events.
func NewController(
	opt rbase.Options,
	aeInformer networkinginformers.ActivationEndpointInformer,
) *controller.Impl {
	c := &reconciler{
		Base:     rbase.NewBase(opt, controllerAgentName),
		aeLister: aeInformer.Lister(),
	}
	impl := controller.NewImpl(c, c.Logger, reconcilerName)

	// Set up all event handlers for resources we are interested in
	c.Logger.Info("Setting up event handlers")

	// We need to watch activator endpoints chanes and update effected actiavtorendpints objects

	return impl
}
