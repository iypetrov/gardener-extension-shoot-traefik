// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package traefik provides resources for deploying Traefik ingress controller
// to shoot clusters as a replacement for nginx-ingress-controller.
package traefik

import (
	"context"
	"fmt"

	resourcesv1alpha1 "github.com/gardener/gardener/pkg/apis/resources/v1alpha1"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/imagevector"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/gardener-extension-shoot-traefik/pkg/apis/config"
)

// Config holds the configuration for the Traefik deployment.
type Config struct {
	// Replicas is the number of Traefik replicas.
	Replicas int32
	// IngressClass is the ingress class name that Traefik will handle.
	IngressClass string
	// IngressProvider specifies which Kubernetes Ingress provider to use.
	IngressProvider config.IngressProviderType
	// LogLevel sets the Traefik log level.
	LogLevel string
}

// DefaultConfig returns the default configuration for Traefik.
func DefaultConfig() Config {
	return Config{
		Replicas:        2,
		IngressClass:    "traefik",
		IngressProvider: config.IngressProviderKubernetesIngress,
		LogLevel:        "INFO",
	}
}

// Deployer handles deploying Traefik resources to shoot clusters.
type Deployer struct {
	client      client.Client
	decoder     runtime.Decoder
	logger      logr.Logger
	config      Config
	imageVector imagevector.ImageVector
}

// NewDeployer creates a new Deployer.
func NewDeployer(c client.Client, logger logr.Logger, cfg Config, imageVector imagevector.ImageVector) *Deployer {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	return &Deployer{
		client:      c,
		decoder:     serializer.NewCodecFactory(scheme).UniversalDecoder(),
		logger:      logger.WithName("traefik-deployer"),
		config:      cfg,
		imageVector: imageVector,
	}
}

// Deploy deploys Traefik to the shoot cluster via a ManagedResource.
func (d *Deployer) Deploy(ctx context.Context, namespace string) error {
	d.logger.Info("deploying traefik to shoot cluster", "namespace", namespace)

	// Generate all Traefik resources
	resources, err := d.generateResources()
	if err != nil {
		return fmt.Errorf("failed to generate traefik resources: %w", err)
	}

	// Compute checksum of resources to ensure changes are detected
	checksum := utils.ComputeSecretChecksum(resources)

	// Create the secret containing the manifests
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ManagedResourceName,
			Namespace: namespace,
			Annotations: map[string]string{
				"resources.gardener.cloud/data-checksum": checksum,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: resources,
	}

	if err := d.client.Create(ctx, secret); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("failed to create secret: %w", err)
		}
		// Update existing secret - fetch first to get resourceVersion
		existing := &corev1.Secret{}
		if err := d.client.Get(ctx, client.ObjectKeyFromObject(secret), existing); err != nil {
			return fmt.Errorf("failed to get existing secret: %w", err)
		}
		secret.ResourceVersion = existing.ResourceVersion
		if err := d.client.Update(ctx, secret); err != nil {
			return fmt.Errorf("failed to update secret: %w", err)
		}
	}

	// Create the ManagedResource
	managedResource := &resourcesv1alpha1.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ManagedResourceName,
			Namespace: namespace,
		},
		Spec: resourcesv1alpha1.ManagedResourceSpec{
			SecretRefs: []corev1.LocalObjectReference{
				{Name: ManagedResourceName},
			},
			InjectLabels: map[string]string{
				"shoot.gardener.cloud/no-cleanup": "true",
			},
			KeepObjects: new(false),
		},
	}

	if err := d.client.Create(ctx, managedResource); err != nil {
		if client.IgnoreAlreadyExists(err) != nil {
			return fmt.Errorf("failed to create managed resource: %w", err)
		}
		// Update existing managed resource - fetch first to get resourceVersion
		existing := &resourcesv1alpha1.ManagedResource{}
		if err := d.client.Get(ctx, client.ObjectKeyFromObject(managedResource), existing); err != nil {
			return fmt.Errorf("failed to get existing managed resource: %w", err)
		}
		managedResource.ResourceVersion = existing.ResourceVersion
		if err := d.client.Update(ctx, managedResource); err != nil {
			return fmt.Errorf("failed to update managed resource: %w", err)
		}
	}

	d.logger.Info("successfully deployed traefik", "namespace", namespace)

	return nil
}

// Delete removes Traefik from the shoot cluster.
func (d *Deployer) Delete(ctx context.Context, namespace string) error {
	d.logger.Info("deleting traefik from shoot cluster", "namespace", namespace)

	// Delete the ManagedResource
	managedResource := &resourcesv1alpha1.ManagedResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ManagedResourceName,
			Namespace: namespace,
		},
	}

	if err := d.client.Delete(ctx, managedResource); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete managed resource: %w", err)
		}
	}

	// Delete the secret
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ManagedResourceName,
			Namespace: namespace,
		},
	}

	if err := d.client.Delete(ctx, secret); err != nil {
		if client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete secret: %w", err)
		}
	}

	d.logger.Info("successfully deleted traefik", "namespace", namespace)

	return nil
}

// generateResources generates all Kubernetes resources for Traefik.
func (d *Deployer) generateResources() (map[string][]byte, error) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	codec := serializer.NewCodecFactory(scheme).LegacyCodec(
		corev1.SchemeGroupVersion,
		appsv1.SchemeGroupVersion,
		rbacv1.SchemeGroupVersion,
		networkingv1.SchemeGroupVersion,
	)

	resources := make(map[string][]byte)

	// ServiceAccount
	sa := d.serviceAccount()
	saData, err := runtime.Encode(codec, sa)
	if err != nil {
		return nil, fmt.Errorf("failed to encode service account: %w", err)
	}
	resources["serviceaccount.yaml"] = saData

	// ClusterRole
	cr := d.clusterRole()
	crData, err := runtime.Encode(codec, cr)
	if err != nil {
		return nil, fmt.Errorf("failed to encode cluster role: %w", err)
	}
	resources["clusterrole.yaml"] = crData

	// ClusterRoleBinding
	crb := d.clusterRoleBinding()
	crbData, err := runtime.Encode(codec, crb)
	if err != nil {
		return nil, fmt.Errorf("failed to encode cluster role binding: %w", err)
	}
	resources["clusterrolebinding.yaml"] = crbData

	// Deployment
	deploy, err := d.deployment()
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment: %w", err)
	}
	deployData, err := runtime.Encode(codec, deploy)
	if err != nil {
		return nil, fmt.Errorf("failed to encode deployment: %w", err)
	}
	resources["deployment.yaml"] = deployData

	// Service
	svc := d.service()
	svcData, err := runtime.Encode(codec, svc)
	if err != nil {
		return nil, fmt.Errorf("failed to encode service: %w", err)
	}
	resources["service.yaml"] = svcData

	// IngressClass
	ic := d.ingressClass()
	icData, err := runtime.Encode(codec, ic)
	if err != nil {
		return nil, fmt.Errorf("failed to encode ingress class: %w", err)
	}
	resources["ingressclass.yaml"] = icData

	// NetworkPolicy
	np := d.networkPolicy()
	npData, err := runtime.Encode(codec, np)
	if err != nil {
		return nil, fmt.Errorf("failed to encode network policy: %w", err)
	}
	resources["networkpolicy.yaml"] = npData

	return resources, nil
}

func (d *Deployer) serviceAccount() *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceAccountName,
			Namespace: Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "traefik",
				"app.kubernetes.io/instance":   "traefik",
				"app.kubernetes.io/component":  "ingress-controller",
				"app.kubernetes.io/managed-by": "gardener",
			},
		},
	}
}

func (d *Deployer) clusterRole() *rbacv1.ClusterRole {
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"services", "endpoints", "secrets", "nodes"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"discovery.k8s.io"},
			Resources: []string{"endpointslices"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"extensions", "networking.k8s.io"},
			Resources: []string{"ingresses", "ingressclasses"},
			Verbs:     []string{"get", "list", "watch"},
		},
		{
			APIGroups: []string{"extensions", "networking.k8s.io"},
			Resources: []string{"ingresses/status"},
			Verbs:     []string{"update"},
		},
		{
			APIGroups: []string{"traefik.io"},
			Resources: []string{"*"},
			Verbs:     []string{"get", "list", "watch"},
		},
	}

	// Add namespace and pod permissions for NGINX provider
	// Namespaces are required for watchNamespaceSelector
	// Pods are required for OTel attributes injection and other functionality
	if d.config.IngressProvider == config.IngressProviderKubernetesIngressNGINX {
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"namespaces"},
			Verbs:     []string{"get", "list", "watch"},
		})
		rules = append(rules, rbacv1.PolicyRule{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get"},
		})
	}

	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "traefik-ingress-controller",
			Labels: map[string]string{
				"app.kubernetes.io/name":       "traefik",
				"app.kubernetes.io/instance":   "traefik",
				"app.kubernetes.io/managed-by": "gardener",
			},
		},
		Rules: rules,
	}
}

func (d *Deployer) clusterRoleBinding() *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "traefik-ingress-controller",
			Labels: map[string]string{
				"app.kubernetes.io/name":       "traefik",
				"app.kubernetes.io/instance":   "traefik",
				"app.kubernetes.io/managed-by": "gardener",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "traefik-ingress-controller",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      ServiceAccountName,
				Namespace: Namespace,
			},
		},
	}
}

func (d *Deployer) deployment() (*appsv1.Deployment, error) {
	labels := map[string]string{
		"app.kubernetes.io/name":                 "traefik",
		"app.kubernetes.io/instance":             "traefik",
		"app.kubernetes.io/component":            "ingress-controller",
		"app.kubernetes.io/managed-by":           "gardener",
		"networking.gardener.cloud/to-apiserver": "allowed",
		"networking.gardener.cloud/to-dns":       "allowed",
	}

	// Get the Traefik image from the image vector
	img, err := d.imageVector.FindImage(ImageName)
	if err != nil {
		return nil, fmt.Errorf("failed to find traefik image in image vector: %w", err)
	}
	image := img.String()

	// Configure Traefik arguments based on the selected provider
	args := []string{
		"--api.insecure=false",
		"--api.dashboard=false",
		"--ping=true",
		"--ping.entrypoint=web",
		"--metrics.prometheus=true",
		"--metrics.prometheus.entrypoint=metrics",
		"--entrypoints.web.address=:8000",
		"--entrypoints.websecure.address=:8443",
		"--entrypoints.metrics.address=:9100",
		fmt.Sprintf("--log.level=%s", d.config.LogLevel),
	}

	// Configure the appropriate Kubernetes Ingress provider
	switch d.config.IngressProvider {
	case config.IngressProviderKubernetesIngressNGINX:
		// Enable NGINX-compatible Ingress provider for migration scenarios
		args = append(args,
			"--providers.kubernetesingressnginx=true",
			fmt.Sprintf("--providers.kubernetesingressnginx.ingressclass=%s", d.config.IngressClass),
		)
	case config.IngressProviderKubernetesIngress:
		fallthrough
	default:
		// Enable standard Kubernetes Ingress provider (default)
		args = append(args,
			"--providers.kubernetesingress=true",
			fmt.Sprintf("--providers.kubernetesingress.ingressclass=%s", d.config.IngressClass),
		)
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName,
			Namespace: Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: new(d.config.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "traefik",
					"app.kubernetes.io/instance": "traefik",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
					Annotations: map[string]string{
						"prometheus.io/scrape": "true",
						"prometheus.io/port":   "9100",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: ServiceAccountName,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: new(true),
						RunAsUser:    new(int64(65532)),
						RunAsGroup:   new(int64(65532)),
						FSGroup:      new(int64(65532)),
					},
					Containers: []corev1.Container{
						{
							Name:  "traefik",
							Image: image,
							Args:  args,
							Ports: []corev1.ContainerPort{
								{
									Name:          "web",
									ContainerPort: 8000,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "websecure",
									ContainerPort: 8443,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "metrics",
									ContainerPort: 9100,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ping",
										Port: intstr.FromInt(8000),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
								TimeoutSeconds:      3,
								FailureThreshold:    12, // Allow up to 60 seconds for startup
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ping",
										Port: intstr.FromInt(8000),
									},
								},
								PeriodSeconds:    10,
								TimeoutSeconds:   5,
								FailureThreshold: 3,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/ping",
										Port: intstr.FromInt(8000),
									},
								},
								PeriodSeconds:    5,
								TimeoutSeconds:   3,
								FailureThreshold: 3,
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("128Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: new(false),
								ReadOnlyRootFilesystem:   new(true),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "KUBERNETES_SERVICE_HOST",
									Value: "kubernetes.default.svc.cluster.local",
								},
								{
									Name:  "KUBERNETES_SERVICE_PORT",
									Value: "443",
								},
							},
						},
					},
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "kubernetes.io/hostname",
							WhenUnsatisfiable: corev1.ScheduleAnyway,
							LabelSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app.kubernetes.io/name":     "traefik",
									"app.kubernetes.io/instance": "traefik",
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func (d *Deployer) service() *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "traefik",
			Namespace: Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "traefik",
				"app.kubernetes.io/instance":   "traefik",
				"app.kubernetes.io/component":  "ingress-controller",
				"app.kubernetes.io/managed-by": "gardener",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				"app.kubernetes.io/name":     "traefik",
				"app.kubernetes.io/instance": "traefik",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "web",
					Port:       80,
					TargetPort: intstr.FromString("web"),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "websecure",
					Port:       443,
					TargetPort: intstr.FromString("websecure"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

func (d *Deployer) ingressClass() *networkingv1.IngressClass {
	// Set the controller based on the ingress provider type
	controller := "traefik.io/ingress-controller"
	if d.config.IngressProvider == config.IngressProviderKubernetesIngressNGINX {
		controller = "k8s.io/ingress-nginx"
	}

	return &networkingv1.IngressClass{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "IngressClass",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: d.config.IngressClass,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "traefik",
				"app.kubernetes.io/instance":   "traefik",
				"app.kubernetes.io/managed-by": "gardener",
			},
			Annotations: map[string]string{
				// Make traefik the default ingress class as a replacement for nginx
				"ingressclass.kubernetes.io/is-default-class": "true",
			},
		},
		Spec: networkingv1.IngressClassSpec{
			Controller: controller,
		},
	}
}

func (d *Deployer) networkPolicy() *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "networking.k8s.io/v1",
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "traefik-allow-ingress",
			Namespace: Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "traefik",
				"app.kubernetes.io/managed-by": "gardener",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "traefik",
					"app.kubernetes.io/instance": "traefik",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					// Allow all ingress traffic to Traefik from anywhere
					// This is required for the LoadBalancer to reach Traefik pods
				},
			},
			// Allow all egress traffic from Traefik to anywhere
			// This is required for Traefik to reach backend pods behind Ingress resources.
			// Think about making this configurable in the future if we want to be more restrictive, but it would require users to add additional policies to allow traffic to their backend pods
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					// Allow egress to all pods in all namespaces
					// This is required for Traefik to reach backend pods behind Ingress resources
					// Note: DNS and API server access is already granted via Gardener's policies
					// (gardener.cloud--allow-to-dns and gardener.cloud--allow-to-apiserver)
					// which match pods with the corresponding labels on the Traefik deployment
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{},
							PodSelector:       &metav1.LabelSelector{},
						},
					},
				},
			},
		},
	}
}
