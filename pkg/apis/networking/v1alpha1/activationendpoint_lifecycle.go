package v1alpha1

import "k8s.io/apimachinery/pkg/runtime/schema"

// GetGroupVersionKind returns the GVK for the ActivationEndpoint.
func (ae *ActivationEndpoint) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("ActivationEndpoint")
}
