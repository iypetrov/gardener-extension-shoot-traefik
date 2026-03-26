// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package traefik provides resources for deploying Traefik ingress controller
// to shoot clusters as a replacement for nginx-ingress-controller.
package traefik

import (
	"context"
	"fmt"
	"time"

	extensionsv1alpha1helper "github.com/gardener/gardener/pkg/api/extensions/v1alpha1/helper"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/utils/imagevector"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/gardener-extension-shoot-traefik/pkg/apis/config"
)

const (
	// ManagedResourceDeletionTimeout is the maximum duration to wait for a
	// ManagedResource to be deleted before timing out.
	ManagedResourceDeletionTimeout = 2 * time.Minute
)

var (
	// shootScheme is a shared scheme for encoding shoot-cluster resources.
	shootScheme *runtime.Scheme
	// shootCodec is a shared codec for encoding shoot-cluster resources.
	shootCodec runtime.Codec
	// extensionsScheme is a shared scheme for encoding extension resources (e.g. DNSRecord).
	extensionsScheme *runtime.Scheme
	// extensionsCodec is a shared codec for encoding extension resources.
	extensionsCodec runtime.Codec
)

func init() {
	shootScheme = runtime.NewScheme()
	_ = corev1.AddToScheme(shootScheme)
	_ = appsv1.AddToScheme(shootScheme)
	_ = rbacv1.AddToScheme(shootScheme)
	_ = networkingv1.AddToScheme(shootScheme)
	_ = policyv1.AddToScheme(shootScheme)
	shootCodec = serializer.NewCodecFactory(shootScheme).LegacyCodec(
		corev1.SchemeGroupVersion,
		appsv1.SchemeGroupVersion,
		rbacv1.SchemeGroupVersion,
		networkingv1.SchemeGroupVersion,
		policyv1.SchemeGroupVersion,
	)

	extensionsScheme = runtime.NewScheme()
	_ = extensionsv1alpha1.AddToScheme(extensionsScheme)
	extensionsCodec = serializer.NewCodecFactory(extensionsScheme).LegacyCodec(extensionsv1alpha1.SchemeGroupVersion)
}

// Config holds the configuration for the Traefik deployment.
type Config struct {
	// Replicas is the number of Traefik replicas.
	Replicas int32
	// IngressProvider specifies which Kubernetes Ingress provider to use.
	IngressProvider config.IngressProviderType
	// LogLevel sets the Traefik log level.
	LogLevel string
}

// DefaultConfig returns the default configuration for Traefik.
func DefaultConfig() Config {
	return Config{
		Replicas:        2,
		IngressProvider: config.IngressProviderKubernetesIngress,
		LogLevel:        "INFO",
	}
}

// IngressClassName returns the ingress class name derived from the configured
// IngressProvider. KubernetesIngressNGINX uses "nginx", all others use "traefik".
func (c Config) IngressClassName() string {
	if c.IngressProvider == config.IngressProviderKubernetesIngressNGINX {
		return "nginx"
	}

	return "traefik"
}

// Deployer handles deploying Traefik resources to shoot clusters.
type Deployer struct {
	client      client.Client
	logger      logr.Logger
	config      Config
	imageVector imagevector.ImageVector
}

// NewDeployer creates a new Deployer.
func NewDeployer(c client.Client, logger logr.Logger, cfg Config, imageVector imagevector.ImageVector) *Deployer {
	return &Deployer{
		client:      c,
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

	// CreateForShoot handles atomic create-or-update of both the backing
	// Secret and the ManagedResource, including the no-cleanup inject label.
	if err := managedresources.CreateForShoot(ctx, d.client, namespace, ManagedResourceName, ManagedResourceName, false, resources); err != nil {
		return fmt.Errorf("failed to create or update managed resource: %w", err)
	}

	d.logger.Info("successfully deployed traefik", "namespace", namespace)

	return nil
}

// Delete removes Traefik from the shoot cluster.
//
// The resources are deleted from the shoot cluster before the ManagedResource
// itself is removed. Use this for normal deletion where the shoot API server
// is still reachable.
//
// The caller MUST NOT return before this function completes: gardenlet's
// "Waiting until shoot managed resources have been deleted" task lists every
// shoot-class (no-class) ManagedResource in the shoot namespace and will time
// out if extension-traefik still exists when that check runs.
func (d *Deployer) Delete(ctx context.Context, namespace string) error {
	return d.deleteManagedResource(ctx, namespace)
}

// DeleteKeepingObjects removes the ManagedResource without deleting the
// underlying shoot-cluster objects. Use this during force-delete or migrate
// where the shoot API server may already be unreachable.
func (d *Deployer) DeleteKeepingObjects(ctx context.Context, namespace string) error {
	if err := managedresources.SetKeepObjects(ctx, d.client, namespace, ManagedResourceName, true); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to set keepObjects on managed resource: %w", err)
	}

	return d.deleteManagedResource(ctx, namespace)
}

func (d *Deployer) deleteManagedResource(ctx context.Context, namespace string) error {
	d.logger.Info("deleting traefik from shoot cluster", "namespace", namespace)

	if err := managedresources.Delete(ctx, d.client, namespace, ManagedResourceName, true); err != nil {
		return fmt.Errorf("failed to delete managed resource: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, ManagedResourceDeletionTimeout)
	defer cancel()

	if err := managedresources.WaitUntilDeleted(timeoutCtx, d.client, namespace, ManagedResourceName); err != nil {
		return fmt.Errorf("timed out waiting for managed resource to be deleted: %w", err)
	}

	d.logger.Info("successfully deleted traefik", "namespace", namespace)

	return nil
}

// DeployDNSRecord creates or updates a seed-class ManagedResource containing a
// DNSRecord for the Traefik ingress wildcard domain ("*.ingress.<shootDomain>").
// The DNSRecord is reconciled by the seed's gardener-resource-manager and then
// picked up by the configured DNS provider extension.
//
// Parameters:
//   - namespace: the shoot's control-plane namespace on the seed
//   - lbAddress: the LoadBalancer IP or hostname of the Traefik Service in the shoot
//   - dnsName: the fully-qualified wildcard domain, e.g. "*.ingress.my-shoot.example.com"
//   - providerType: the DNS provider type, e.g. "aws-route53"
//   - secretRef: reference to the DNS provider credentials secret (in the same namespace)
func (d *Deployer) DeployDNSRecord(ctx context.Context, namespace, lbAddress, dnsName, providerType string, secretRef corev1.SecretReference) error {
	d.logger.Info("deploying seed DNSRecord for traefik ingress", "namespace", namespace, "dnsName", dnsName, "lbAddress", lbAddress)

	recordType := extensionsv1alpha1helper.GetDNSRecordType(lbAddress)

	dnsRecord := &extensionsv1alpha1.DNSRecord{
		TypeMeta: metav1.TypeMeta{
			APIVersion: extensionsv1alpha1.SchemeGroupVersion.String(),
			Kind:       extensionsv1alpha1.DNSRecordResource,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      SeedManagedResourceName,
			Namespace: namespace,
		},
		Spec: extensionsv1alpha1.DNSRecordSpec{
			DefaultSpec: extensionsv1alpha1.DefaultSpec{
				Type: providerType,
			},
			SecretRef:  secretRef,
			Name:       dnsName,
			RecordType: recordType,
			Values:     []string{lbAddress},
		},
	}

	dnsRecordData, err := runtime.Encode(extensionsCodec, dnsRecord)
	if err != nil {
		return fmt.Errorf("failed to encode DNSRecord: %w", err)
	}

	if err := managedresources.CreateForSeed(ctx, d.client, namespace, SeedManagedResourceName, false, map[string][]byte{
		"dnsrecord.yaml": dnsRecordData,
	}); err != nil {
		return fmt.Errorf("failed to deploy seed ManagedResource for DNSRecord: %w", err)
	}

	d.logger.Info("successfully deployed seed DNSRecord for traefik ingress", "namespace", namespace)

	return nil
}

// DeleteDNSRecord deletes the Traefik ingress DNSRecord from the seed and then
// cleans up the seed-class ManagedResource that originally created it.
//
// The DNSRecord is deleted directly (not via the ManagedResource) because
// resource-manager's reconciliation loop would revert the deletion-confirmation
// annotation before processing the MR deletion, causing the
// cr-deletion-protection webhook to block cleanup indefinitely.
func (d *Deployer) DeleteDNSRecord(ctx context.Context, namespace string) error {
	d.logger.Info("deleting seed DNSRecord for traefik ingress", "namespace", namespace)

	// 1. Set keepObjects=true on the seed ManagedResource and delete it first,
	//    so resource-manager stops managing the DNSRecord and won't recreate it.
	if err := managedresources.SetKeepObjects(ctx, d.client, namespace, SeedManagedResourceName, true); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to set keepObjects on seed ManagedResource: %w", err)
	}

	if err := managedresources.Delete(ctx, d.client, namespace, SeedManagedResourceName, true); err != nil {
		return fmt.Errorf("failed to delete seed ManagedResource for DNSRecord: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, ManagedResourceDeletionTimeout)
	defer cancel()

	if err := managedresources.WaitUntilDeleted(timeoutCtx, d.client, namespace, SeedManagedResourceName); err != nil {
		return fmt.Errorf("timed out waiting for seed ManagedResource to be deleted: %w", err)
	}

	// 2. Now that resource-manager is no longer managing the DNSRecord, delete it
	//    directly with the deletion-confirmation annotation.
	dnsRecord := &extensionsv1alpha1.DNSRecord{}
	key := client.ObjectKey{Namespace: namespace, Name: SeedManagedResourceName}
	if err := d.client.Get(ctx, key, dnsRecord); err == nil {
		patch := client.MergeFrom(dnsRecord.DeepCopy())
		if dnsRecord.Annotations == nil {
			dnsRecord.Annotations = make(map[string]string)
		}
		dnsRecord.Annotations[v1beta1constants.ConfirmationDeletion] = "true"
		if err := d.client.Patch(ctx, dnsRecord, patch); err != nil {
			return fmt.Errorf("failed to annotate DNSRecord with deletion confirmation: %w", err)
		}

		if err := d.client.Delete(ctx, dnsRecord); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("failed to delete DNSRecord: %w", err)
		}
		d.logger.Info("DNSRecord deleted", "namespace", namespace, "name", SeedManagedResourceName)
	} else if client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to get DNSRecord: %w", err)
	}

	d.logger.Info("successfully deleted seed DNSRecord for traefik ingress", "namespace", namespace)

	return nil
}

// generateResources generates all Kubernetes resources for Traefik.
func (d *Deployer) generateResources() (map[string][]byte, error) {
	resources := make(map[string][]byte)

	// ServiceAccount
	sa := d.serviceAccount()
	saData, err := runtime.Encode(shootCodec, sa)
	if err != nil {
		return nil, fmt.Errorf("failed to encode service account: %w", err)
	}
	resources["serviceaccount.yaml"] = saData

	// ClusterRole
	cr := d.clusterRole()
	crData, err := runtime.Encode(shootCodec, cr)
	if err != nil {
		return nil, fmt.Errorf("failed to encode cluster role: %w", err)
	}
	resources["clusterrole.yaml"] = crData

	// ClusterRoleBinding
	crb := d.clusterRoleBinding()
	crbData, err := runtime.Encode(shootCodec, crb)
	if err != nil {
		return nil, fmt.Errorf("failed to encode cluster role binding: %w", err)
	}
	resources["clusterrolebinding.yaml"] = crbData

	// Deployment
	deploy, err := d.deployment()
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment: %w", err)
	}
	deployData, err := runtime.Encode(shootCodec, deploy)
	if err != nil {
		return nil, fmt.Errorf("failed to encode deployment: %w", err)
	}
	resources["deployment.yaml"] = deployData

	// Service
	svc := d.service()
	svcData, err := runtime.Encode(shootCodec, svc)
	if err != nil {
		return nil, fmt.Errorf("failed to encode service: %w", err)
	}
	resources["service.yaml"] = svcData

	// IngressClass
	ic := d.ingressClass()
	icData, err := runtime.Encode(shootCodec, ic)
	if err != nil {
		return nil, fmt.Errorf("failed to encode ingress class: %w", err)
	}
	resources["ingressclass.yaml"] = icData

	// NetworkPolicy
	np := d.networkPolicy()
	npData, err := runtime.Encode(shootCodec, np)
	if err != nil {
		return nil, fmt.Errorf("failed to encode network policy: %w", err)
	}
	resources["networkpolicy.yaml"] = npData

	// PodDisruptionBudget
	pdb := d.podDisruptionBudget()
	pdbData, err := runtime.Encode(shootCodec, pdb)
	if err != nil {
		return nil, fmt.Errorf("failed to encode pod disruption budget: %w", err)
	}
	resources["poddisruptionbudget.yaml"] = pdbData

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
			Resources: []string{
				"ingressroutes",
				"ingressroutetcps",
				"ingressrouteudps",
				"middlewares",
				"middlewaretcps",
				"serverstransports",
				"serverstransporttcps",
				"tlsoptions",
				"tlsstores",
				"traefikservices",
			},
			Verbs: []string{"get", "list", "watch"},
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

	ingressClass := d.config.IngressClassName()

	if d.config.IngressProvider == config.IngressProviderKubernetesIngress || d.config.IngressProvider == "" {
		args = append(args,
			"--providers.kubernetesingress=true",
			fmt.Sprintf("--providers.kubernetesingress.ingressclass=%s", ingressClass),
		)
	}

	if d.config.IngressProvider == config.IngressProviderKubernetesIngressNGINX {
		args = append(args,
			"--providers.kubernetesingressnginx=true",
			fmt.Sprintf("--providers.kubernetesingressnginx.ingressclass=%s", ingressClass),
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
	// Use the appropriate controller value based on the ingress provider.
	// The kubernetesingress provider filters IngressClasses by "traefik.io/ingress-controller".
	// The kubernetesingressnginx provider expects "k8s.io/ingress-nginx" to be
	// compatible with existing Ingress resources that were created for nginx-ingress-controller.
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
			Name: d.config.IngressClassName(),
			Labels: map[string]string{
				"app.kubernetes.io/name":       "traefik",
				"app.kubernetes.io/instance":   "traefik",
				"app.kubernetes.io/managed-by": "gardener",
			},
			Annotations: map[string]string{
				// Make traefik the default ingress class as a replacement for nginx
				"ingressclass.kubernetes.io/is-default-class": "true",
				// spec.controller is immutable — tell resource-manager to delete
				// and recreate the IngressClass if the value changes.
				"resources.gardener.cloud/delete-on-invalid-update": "true",
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

func (d *Deployer) podDisruptionBudget() *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "policy/v1",
			Kind:       "PodDisruptionBudget",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName,
			Namespace: Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "traefik",
				"app.kubernetes.io/instance":   "traefik",
				"app.kubernetes.io/managed-by": "gardener",
			},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name":     "traefik",
					"app.kubernetes.io/instance": "traefik",
				},
			},
		},
	}
}
