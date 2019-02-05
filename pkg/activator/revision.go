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

package activator

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"time"

	"github.com/knative/pkg/logging/logkey"
	"github.com/knative/serving/pkg/apis/serving"
	"github.com/knative/serving/pkg/apis/serving/v1alpha1"
	clientset "github.com/knative/serving/pkg/client/clientset/versioned"
	revisionresources "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources"
	revisionresourcenames "github.com/knative/serving/pkg/reconciler/v1alpha1/revision/resources/names"
	"github.com/knative/serving/pkg/tracing"
	"github.com/knative/serving/pkg/utils"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

var _ Activator = (*revisionActivator)(nil)

type revisionActivator struct {
	readyTimout time.Duration // For testing.
	kubeClient  kubernetes.Interface
	knaClient   clientset.Interface
	logger      *zap.SugaredLogger
	tracer      opentracing.Tracer
}

// NewRevisionActivator creates an Activator that changes revision
// serving status to active if necessary, then returns the endpoint
// once the revision is ready to serve traffic.
func NewRevisionActivator(kubeClient kubernetes.Interface, servingClient clientset.Interface, logger *zap.SugaredLogger) Activator {
	return &revisionActivator{
		readyTimout: 60 * time.Second,
		kubeClient:  kubeClient,
		knaClient:   servingClient,
		logger:      logger,
	}
}

func (r *revisionActivator) Shutdown() {
	// Nothing to do.
}

type watchable interface {
	Watch(opts metav1.ListOptions) (watch.Interface, error)
}

type traceState struct {
	revCreating        opentracing.Span
	depCreating        opentracing.Span
	podCreating        opentracing.Span
	podPending         opentracing.Span
	initContainerSpans map[string]opentracing.Span
	containerSpans     map[string]opentracing.Span
}

type traceEvHandler func(tracer opentracing.Tracer, event watch.Event, state *traceState)

func cleanupSpan(span opentracing.Span) {
	if span != nil {
		span.Finish()
	}
}

func (r *revisionActivator) traceActivate(doneCh <-chan struct{}, namespace, name string, ctx context.Context) error {
	watchCases := []struct {
		src      watchable
		options  metav1.ListOptions
		resource string
		handler  traceEvHandler
	}{
		{
			src:      r.knaClient.ServingV1alpha1().Revisions(namespace),
			options:  metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s", name)},
			resource: "revision",
			handler: func(tracer opentracing.Tracer, event watch.Event, state *traceState) {
				if revision, ok := event.Object.(*v1alpha1.Revision); ok {
					if state.revCreating == nil {
						r.logger.Infof("Starting revision creating span")
						_, state.revCreating = tracing.CreateChildSpanFromContext(tracer, ctx, "revision_creating")
					}
					if !revision.Status.IsActivationRequired() {
						r.logger.Infof("Stopping revision creating span")
						state.revCreating.Finish()
					}
				} else {
					r.logger.Infof("Got non-revision in revision handler: %v", event)
				}
			},
		}, {
			src:      r.kubeClient.AppsV1().Deployments(namespace),
			options:  metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s-deployment", name)},
			resource: "deployment",
			handler: func(tracer opentracing.Tracer, event watch.Event, state *traceState) {
				if deployment, ok := event.Object.(*appsv1.Deployment); ok {
					if *deployment.Spec.Replicas > 0 {
						if state.depCreating == nil {
							r.logger.Infof("Starting deploy creating span")
							_, state.depCreating = tracing.CreateChildSpanFromContext(tracer, ctx, "deployment_creating")
						} else {
							for _, cond := range deployment.Status.Conditions {
								if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionTrue {
									r.logger.Infof("Stopping deploy creating span")
									state.depCreating.Finish()
								}
							}
						}
					}
				}
			},
		}, {
			src:      r.kubeClient.CoreV1().Pods(namespace),
			options:  metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", name)},
			resource: "pod",
			handler: func(tracer opentracing.Tracer, event watch.Event, state *traceState) {
				if pod, ok := event.Object.(*corev1.Pod); ok {
					if state.podCreating == nil {
						r.logger.Infof("Starting pod creating span")
						_, state.podCreating = tracing.CreateChildSpanFromContext(tracer, ctx, "pod_creating")
					}
					if pod.Status.Phase == corev1.PodPending && state.podPending == nil {
						_, state.podPending = tracing.CreateChildSpanFromContext(tracer, ctx, "pod_pending")
					} else if pod.Status.Phase == corev1.PodRunning && state.podPending != nil {
						state.podPending.Finish()
					}

					for _, csCase := range []struct {
						src  []corev1.ContainerStatus
						dest map[string]opentracing.Span
					}{{
						src:  pod.Status.InitContainerStatuses,
						dest: state.initContainerSpans,
					}, {
						src:  pod.Status.ContainerStatuses,
						dest: state.containerSpans,
					}} {
						for _, cs := range csCase.src {
							if cs.State.Waiting != nil {
								if _, ok := csCase.dest[cs.Name]; !ok {
									r.logger.Infof("container %q waiting", cs.Name)
									_, span := tracing.CreateChildSpanFromContext(tracer, ctx, fmt.Sprintf("container_waiting_%s", cs.Name))
									csCase.dest[cs.Name] = span
								}
							} else if cs.State.Terminated != nil || cs.State.Running != nil {
								r.logger.Infof("container %q terminated", cs.Name)
								if span, ok := csCase.dest[cs.Name]; ok {
									span.Finish()
								} else {
									_, span := tracing.CreateChildSpanFromContext(tracer, ctx, fmt.Sprintf("container_waiting_%s", cs.Name))
									csCase.dest[cs.Name] = span
									span.Finish()
								}
							}
						}
					}
				} else {
					r.logger.Infof("Got non-pod in revision handler: %v", event)
				}
			},
		},
	}

	evHandlers := make([]traceEvHandler, len(watchCases))
	cases := make([]reflect.SelectCase, len(watchCases)+1)
	for i, wc := range watchCases {
		wi, err := wc.src.Watch(wc.options)
		if err != nil {
			return fmt.Errorf("Failed to watch %s", wc.resource)
		}
		defer wi.Stop()
		evHandlers[i] = wc.handler
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(wi.ResultChan())}
	}
	cases[len(watchCases)] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(doneCh)}

	state := traceState{
		initContainerSpans: make(map[string]opentracing.Span),
		containerSpans:     make(map[string]opentracing.Span),
	}
	defer func() {
		r.logger.Infof("Stopping revision creating span")
		state.revCreating.Finish()
	}()
	defer func() {
		r.logger.Infof("Stopping pod creating span")
		state.podCreating.Finish()
	}()
	defer func() {
		for _, span := range state.containerSpans {
			span.Finish()
		}
	}()
	tracer := tracing.FromContext(ctx).Tracer
	for {
		chosen, value, ok := reflect.Select(cases)
		if !ok {
			if chosen == len(watchCases) {
				// doneCh closed
				break
			} else {
				cases[chosen].Chan = reflect.ValueOf(nil)
			}
		} else {
			evHandlers[chosen](tracer, value.Interface().(watch.Event), &state)
		}
	}
	return nil

	/*
		depWi, err := r.kubeClient.AppsV1().Deployments(rev.namespace).Watch(metav1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", rev.name+"-deployment"),
		})
		if err != nil {
			return nil, errors.New("failed to watch the deployment")
		}
		defer depWi.Stop()
		depCh := depWi.ResultChan()

		podWi, err := r.kubeClient.CoreV1().Pods(rev.namespace).Watch(metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app=%s", rev.name),
		})
		if err != nil {
			return nil, errors.New("failed to watch the app pod")
		}
		defer podWi.Stop()
		podCh := podWi.ResultChan()

		tracer := tracing.FromContext(ctx).Tracer
		_, revCreateSpan := tracing.CreateChildSpanFromContext(tracer, ctx, "revision_creating")

	*/
}

func (r *revisionActivator) activateRevision(namespace, name, key string, ctx context.Context) (*v1alpha1.Revision, error) {
	logger := r.logger.With(zap.String(logkey.Key, key))
	rev := revisionID{
		namespace: namespace,
		name:      name,
	}

	// Get the current revision serving state
	revisionClient := r.knaClient.ServingV1alpha1().Revisions(rev.namespace)
	revision, err := revisionClient.Get(rev.name, metav1.GetOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "unable to get the revision")
	}

	// Wait for the revision to not require activation.
	if revision.Status.IsActivationRequired() {
		traceDoneCh := make(chan struct{})
		defer close(traceDoneCh)
		go r.traceActivate(traceDoneCh, namespace, name, ctx)
		wi, err := r.knaClient.ServingV1alpha1().Revisions(rev.namespace).Watch(metav1.ListOptions{
			FieldSelector: fmt.Sprintf("metadata.name=%s", rev.name),
		})
		if err != nil {
			return nil, errors.New("failed to watch the revision")
		}
		defer wi.Stop()
		revCh := wi.ResultChan()
	RevisionActive:
		for {
			select {
			case <-time.After(r.readyTimout):
				// last chance to check
				if !revision.Status.IsActivationRequired() {
					break RevisionActive
				}
				return nil, errors.New("timeout waiting for the revision to become ready")
			case event := <-revCh:
				if revision, ok := event.Object.(*v1alpha1.Revision); ok {
					if revision.Status.IsActivationRequired() {
						logger.Infof("Revision %s is not yet ready. Got event %v.", name, event)
						continue
					} else {
						logger.Infof("Revision %s is ready", name)
					}
					break RevisionActive
				} else {
					return nil, fmt.Errorf("unexpected result type for the revision: %v", event)
				}
			}
		}
	}
	return revision, nil
}

func (r *revisionActivator) revisionEndpoint(revision *v1alpha1.Revision) (end Endpoint, err error) {
	services := r.kubeClient.CoreV1().Services(revision.GetNamespace())
	serviceName := revisionresourcenames.K8sService(revision)
	svc, err := services.Get(serviceName, metav1.GetOptions{})
	if err != nil {
		return end, errors.Wrapf(err, "unable to get service %s for revision", serviceName)
	}

	fqdn := fmt.Sprintf("%s.%s.svc.%s", serviceName, revision.Namespace, utils.GetClusterDomainName())

	// Search for the correct port in all the service ports.
	port := int32(-1)
	for _, p := range svc.Spec.Ports {
		if p.Name == revisionresources.ServicePortName {
			port = p.Port
			break
		}
	}
	if port == -1 {
		return end, errors.New("revision needs external HTTP port")
	}

	return Endpoint{
		FQDN: fqdn,
		Port: port,
	}, nil
}

// ActiveEndpoint activates the revision `name` and returnts the result.
func (r *revisionActivator) ActiveEndpoint(namespace, name string, ctx context.Context) ActivationResult {
	key := fmt.Sprintf("%s/%s", namespace, name)
	logger := r.logger.With(zap.String(logkey.Key, key))
	revision, err := r.activateRevision(namespace, name, key, ctx)
	if err != nil {
		logger.Errorw("Failed to activate the revision.", zap.Error(err))
		return ActivationResult{
			Status: http.StatusInternalServerError,
			Error:  err,
		}
	}

	serviceName, configurationName := getServiceAndConfigurationLabels(revision)
	endpoint, err := r.revisionEndpoint(revision)
	if err != nil {
		logger.Errorw("Failed to get revision endpoint.", zap.Error(err))
		return ActivationResult{
			Status:            http.StatusInternalServerError,
			ServiceName:       serviceName,
			ConfigurationName: configurationName,
			Error:             err,
		}
	}

	return ActivationResult{
		Status:            http.StatusOK,
		Endpoint:          endpoint,
		ServiceName:       serviceName,
		ConfigurationName: configurationName,
		Error:             nil,
	}
}

func getServiceAndConfigurationLabels(rev *v1alpha1.Revision) (string, string) {
	if rev.Labels == nil {
		return "", ""
	}
	return rev.Labels[serving.ServiceLabelKey], rev.Labels[serving.ConfigurationLabelKey]
}
