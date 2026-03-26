// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IngressProviderType defines the type of Kubernetes Ingress provider to use.
type IngressProviderType string

const (
	// IngressProviderKubernetesIngress is the standard Kubernetes Ingress provider.
	IngressProviderKubernetesIngress IngressProviderType = "KubernetesIngress"
	// IngressProviderKubernetesIngressNGINX is the NGINX-compatible Kubernetes Ingress provider.
	// This provider supports NGINX Ingress Controller annotations, making it easier to migrate
	// from NGINX Ingress Controller to Traefik.
	IngressProviderKubernetesIngressNGINX IngressProviderType = "KubernetesIngressNGINX"
)

// TraefikConfigSpec defines the desired state of [TraefikConfig]
type TraefikConfigSpec struct {
	// Replicas is the number of Traefik replicas to deploy.
	// Defaults to 2 if not specified.
	Replicas int32 `json:"replicas,omitempty"`

	// IngressProvider specifies which Kubernetes Ingress provider to use.
	// Valid values are:
	// - "KubernetesIngress" (default): Standard Kubernetes Ingress provider
	// - "KubernetesIngressNGINX": NGINX-compatible provider with support for NGINX annotations
	//
	// Use KubernetesIngressNGINX when migrating from NGINX Ingress Controller to maintain
	// compatibility with existing NGINX-specific annotations.
	IngressProvider IngressProviderType `json:"ingressProvider,omitempty"`

	// LogLevel sets the Traefik log level.
	// Valid values are: DEBUG, INFO, WARN, ERROR, FATAL, PANIC
	// Defaults to "INFO" if not specified.
	LogLevel string `json:"logLevel,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// TraefikConfig is the configuration schema for the Traefik extension.
// This extension deploys Traefik ingress controller to shoot clusters
// as a replacement for the nginx-ingress-controller which is out of maintenance.
type TraefikConfig struct {
	metav1.TypeMeta `json:",inline"`

	// Spec provides the Traefik extension configuration spec.
	Spec TraefikConfigSpec `json:"spec"`
}
