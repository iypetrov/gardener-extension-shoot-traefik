// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

// Package actuator provides the implementation of a Gardener extension
// actuator for deploying Traefik ingress controller to shoot clusters.
package actuator

import (
	"context"
	"errors"
	"fmt"

	extensionsconfigv1alpha1 "github.com/gardener/gardener/extensions/pkg/apis/config/v1alpha1"
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/extension"
	extensionsutil "github.com/gardener/gardener/extensions/pkg/util"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	gardenerutils "github.com/gardener/gardener/pkg/utils/gardener"
	"github.com/gardener/gardener/pkg/utils/imagevector"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/component-base/featuregate"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/gardener-extension-shoot-traefik/pkg/apis/config"
	"github.com/gardener/gardener-extension-shoot-traefik/pkg/metrics"
	"github.com/gardener/gardener-extension-shoot-traefik/pkg/traefik"
)

// ErrInvalidActuator is an error which is returned when creating an [Actuator]
// with invalid config settings.
var ErrInvalidActuator = errors.New("invalid actuator")

// ErrShootPurposeNotEvaluation is returned when attempting to deploy Traefik
// to a shoot that does not have purpose "evaluation".
var ErrShootPurposeNotEvaluation = errors.New("shoot purpose must be 'evaluation' for traefik extension")

const (
	// Name is the name of the actuator
	Name = "traefik"
	// ExtensionType is the type of the extension resources, which the
	// actuator reconciles.
	ExtensionType = "traefik"
	// FinalizerSuffix is the finalizer suffix used by the actuator
	FinalizerSuffix = "gardener-extension-shoot-traefik"
)

// Actuator is an implementation of [extension.Actuator].
type Actuator struct {
	client      client.Client
	decoder     runtime.Decoder
	imageVector imagevector.ImageVector

	// The following fields are usually derived from the list of extra Helm
	// values provided by gardenlet during the deployment of the extension.
	//
	// See the link below for more details about how gardenlet provides
	// extra values to Helm during the extension deployment.
	//
	// https://github.com/gardener/gardener/blob/d5071c800378616eb6bb2c7662b4b28f4cfe7406/pkg/gardenlet/controller/controllerinstallation/controllerinstallation/reconciler.go#L236-L263
	gardenerVersion       string
	gardenletFeatureGates map[featuregate.Feature]bool
}

var _ extension.Actuator = &Actuator{}

// Option is a function, which configures the [Actuator].
type Option func(a *Actuator) error

// New creates a new actuator with the given options.
func New(c client.Client, imageVector imagevector.ImageVector, opts ...Option) (*Actuator, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: no client specified", ErrInvalidActuator)
	}
	if imageVector == nil {
		return nil, fmt.Errorf("%w: no image vector specified", ErrInvalidActuator)
	}

	act := &Actuator{
		client:                c,
		imageVector:           imageVector,
		gardenletFeatureGates: make(map[featuregate.Feature]bool),
	}

	for _, opt := range opts {
		if err := opt(act); err != nil {
			return nil, err
		}
	}

	if act.decoder == nil {
		act.decoder = serializer.NewCodecFactory(c.Scheme(), serializer.EnableStrict).UniversalDecoder()
	}

	return act, nil
}

// WithDecoder is an [Option], which configures the [Actuator] with the given
// [runtime.Decoder].
func WithDecoder(d runtime.Decoder) Option {
	opt := func(a *Actuator) error {
		a.decoder = d

		return nil
	}

	return opt
}

// WithGardenerVersion is an [Option], which configures the [Actuator] with the
// given version of Gardener. This version of Gardener is usually provided by
// the gardenlet as part of the extra Helm values during deployment of the
// extension.
func WithGardenerVersion(v string) Option {
	opt := func(a *Actuator) error {
		a.gardenerVersion = v

		return nil
	}

	return opt
}

// WithGardenletFeatures is an [Option], which configures the [Actuator] with
// the given gardenlet feature gates. These feature gates are usually provided
// by the gardenlet as part of the extra Helm values during deployment of the
// extension.
func WithGardenletFeatures(feats map[featuregate.Feature]bool) Option {
	opt := func(a *Actuator) error {
		a.gardenletFeatureGates = feats

		return nil
	}

	return opt
}

// Name returns the name of the actuator. This name can be used when registering
// a controller for the actuator.
func (a *Actuator) Name() string {
	return Name
}

// FinalizerSuffix returns the finalizer suffix to use for the actuator. The
// result of this method may be used when registering a controller with the
// actuator.
func (a *Actuator) FinalizerSuffix() string {
	return FinalizerSuffix
}

// ExtensionType returns the type of extension resources the actuator
// reconciles. The result of this method may be used when registering a
// controller with the actuator.
func (a *Actuator) ExtensionType() string {
	return ExtensionType
}

// ExtensionClass returns the [extensionsv1alpha1.ExtensionClass] for the
// actuator. The result of this method may be used when registering a controller
// with the actuator.
func (a *Actuator) ExtensionClass() extensionsv1alpha1.ExtensionClass {
	return extensionsv1alpha1.ExtensionClassShoot
}

// Reconcile reconciles the [extensionsv1alpha1.Extension] resource by taking
// care of any resources managed by the [Actuator]. This method implements the
// [extension.Actuator] interface.
//
// For the Traefik extension, this deploys Traefik ingress controller to the
// shoot cluster as a replacement for nginx-ingress-controller.
func (a *Actuator) Reconcile(ctx context.Context, logger logr.Logger, ex *extensionsv1alpha1.Extension) error {
	// The cluster name is the same as the name of the namespace for our
	// [extensionsv1alpha1.Extension] resource.
	clusterName := ex.Namespace

	defer func() {
		metrics.ActuatorOperationTotal.WithLabelValues(clusterName, "reconcile").Inc()
	}()

	logger.Info("reconciling traefik extension", "name", ex.Name, "cluster", clusterName)

	cluster, err := extensionscontroller.GetCluster(ctx, a.client, clusterName)
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	if cluster.Shoot.DeletionTimestamp != nil {
		logger.Info("shoot is being deleted, skipping traefik reconciliation", "cluster", clusterName)

		return nil
	}

	if v1beta1helper.HibernationIsEnabled(cluster.Shoot) {
		logger.Info("shoot is hibernated, skipping traefik deployment", "cluster", clusterName)

		return nil
	}

	// Validate that the shoot purpose is "evaluation".
	// This is a defense-in-depth check - the admission webhook should already
	// have validated this, but we check again here to be safe.
	if cluster.Shoot.Spec.Purpose == nil || *cluster.Shoot.Spec.Purpose != gardencorev1beta1.ShootPurposeEvaluation {
		purposeStr := "nil"
		if cluster.Shoot.Spec.Purpose != nil {
			purposeStr = string(*cluster.Shoot.Spec.Purpose)
		}
		logger.Error(ErrShootPurposeNotEvaluation, "shoot purpose validation failed",
			"cluster", clusterName,
			"purpose", purposeStr,
		)

		return fmt.Errorf("%w: got purpose '%s'", ErrShootPurposeNotEvaluation, purposeStr)
	}

	traefikConfig := traefik.DefaultConfig()
	if ex.Spec.ProviderConfig != nil {
		var cfg config.TraefikConfig
		if err := runtime.DecodeInto(a.decoder, ex.Spec.ProviderConfig.Raw, &cfg); err != nil {
			logger.Error(err, "failed to decode provider config, using defaults")
		} else {
			// Apply custom configuration
			if cfg.Spec.Replicas > 0 {
				traefikConfig.Replicas = cfg.Spec.Replicas
			}
			if cfg.Spec.IngressClass != "" {
				traefikConfig.IngressClass = cfg.Spec.IngressClass
			}
			if cfg.Spec.IngressProvider != "" {
				traefikConfig.IngressProvider = cfg.Spec.IngressProvider
			}
			if cfg.Spec.LogLevel != "" {
				traefikConfig.LogLevel = cfg.Spec.LogLevel
			}

			// Auto-set IngressClass to "nginx" for NGINX compatibility mode if not explicitly set
			if cfg.Spec.IngressProvider == config.IngressProviderKubernetesIngressNGINX && cfg.Spec.IngressClass == "" {
				traefikConfig.IngressClass = "nginx"
			}
		}
	}

	deployer := traefik.NewDeployer(a.client, logger, traefikConfig, a.imageVector)
	if err := deployer.Deploy(ctx, clusterName); err != nil {
		return fmt.Errorf("failed to deploy traefik: %w", err)
	}

	// Deploy the DNSRecord for the Traefik ingress wildcard domain via a seed ManagedResource.
	if err := a.reconcileDNSRecord(ctx, logger, cluster, clusterName, deployer); err != nil {
		return err
	}

	logger.Info("successfully reconciled traefik extension", "cluster", clusterName)

	return nil
}

// reconcileDNSRecord reads the Traefik LoadBalancer address from the shoot
// cluster and creates/updates the seed-class ManagedResource containing the
// DNSRecord for the wildcard ingress domain.
func (a *Actuator) reconcileDNSRecord(ctx context.Context, logger logr.Logger, cluster *extensionscontroller.Cluster, clusterName string, deployer *traefik.Deployer) error {
	shoot := cluster.Shoot

	// Skip DNS record creation when no DNS domain is configured for the shoot.
	if shoot.Spec.DNS == nil || shoot.Spec.DNS.Domain == nil {
		logger.Info("shoot has no DNS domain configured, skipping ingress DNS record", "cluster", clusterName)

		return nil
	}

	// Reuse the provider type and credentials secret from the external DNSRecord
	// that gardenlet already created for the shoot's API server endpoint.
	// That record is named "<shootName>-external" in the shoot's control-plane namespace.
	ref, err := externalDNSRecordRef(ctx, a.client, clusterName, shoot.Name)
	if err != nil {
		return fmt.Errorf("failed to look up external DNSRecord for shoot: %w", err)
	}
	if ref == nil {
		logger.Info("external DNSRecord not yet available for shoot, skipping ingress DNS record", "cluster", clusterName)

		return nil
	}

	// Build a client for the shoot cluster to read the Traefik Service.
	_, shootClient, err := extensionsutil.NewClientForShoot(ctx, a.client, clusterName, client.Options{}, extensionsconfigv1alpha1.RESTOptions{})
	if err != nil {
		return fmt.Errorf("failed to create shoot client: %w", err)
	}

	svc := &corev1.Service{}
	if err := shootClient.Get(ctx, client.ObjectKey{Namespace: traefik.Namespace, Name: traefik.DeploymentName}, svc); err != nil {
		return fmt.Errorf("failed to get traefik service from shoot: %w", err)
	}

	// Determine the LB address – the Service may still be pending.
	lbAddress := lbAddressFromService(svc)
	if lbAddress == "" {
		return errors.New("traefik LoadBalancer address not yet available, will retry")
	}

	dnsName := fmt.Sprintf("*.%s.%s", gardenerutils.IngressPrefix, *shoot.Spec.DNS.Domain)

	return deployer.DeployDNSRecord(ctx, clusterName, lbAddress, dnsName, ref.ProviderType, ref.SecretRef)
}

// dnsRecordRef holds the DNS provider type and credentials secret reference
// extracted from a DNSRecord resource.
type dnsRecordRef struct {
	ProviderType string
	SecretRef    corev1.SecretReference
}

// externalDNSRecordRef looks up the DNSRecord that gardenlet created for the
// shoot's external API server endpoint ("<shootName>-external") and extracts
// the provider type and credentials secretRef from it.
func externalDNSRecordRef(ctx context.Context, c client.Client, namespace, shootName string) (*dnsRecordRef, error) {
	record := &extensionsv1alpha1.DNSRecord{}
	if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: shootName + "-external"}, record); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil, nil
		}

		return nil, err
	}

	return &dnsRecordRef{ProviderType: record.Spec.Type, SecretRef: record.Spec.SecretRef}, nil
}

// lbAddressFromService extracts the first LoadBalancer IP or hostname from a Service.
func lbAddressFromService(svc *corev1.Service) string {
	for _, ing := range svc.Status.LoadBalancer.Ingress {
		if ing.IP != "" {
			return ing.IP
		}
		if ing.Hostname != "" {
			return ing.Hostname
		}
	}

	return ""
}

// Delete deletes any resources managed by the [Actuator]. This method
// implements the [extension.Actuator] interface.
//
// Because the extension is configured with lifecycle.delete=BeforeKubeAPIServer,
// the shoot kube-apiserver is still running at this point. We pass keepObjects=false
// so that resource-manager cleanly removes the Traefik objects from the shoot cluster
// before the ManagedResource itself is deleted. The seed-side DNSRecord ManagedResource
// is deleted first so that the DNS extension cleans up the actual DNS record.
func (a *Actuator) Delete(ctx context.Context, logger logr.Logger, ex *extensionsv1alpha1.Extension) error {
	clusterName := ex.Namespace

	defer func() {
		metrics.ActuatorOperationTotal.WithLabelValues(clusterName, "delete").Inc()
	}()

	logger.Info("deleting traefik resources managed by extension", "cluster", clusterName)

	deployer := traefik.NewDeployer(a.client, logger, traefik.DefaultConfig(), a.imageVector)

	// First delete the DNSRecord ManagedResource from the seed and wait for the
	// DNS extension to clean up the actual DNS record.
	if err := deployer.DeleteDNSRecord(ctx, clusterName); err != nil {
		return fmt.Errorf("failed to delete traefik ingress DNS record: %w", err)
	}

	// Delete the shoot ManagedResource with keepObjects=false. The shoot
	// kube-apiserver is still running at this point (delete: BeforeKubeAPIServer),
	// so resource-manager can cleanly remove Traefik from the shoot cluster.
	if err := deployer.Delete(ctx, clusterName, false); err != nil {
		return fmt.Errorf("failed to delete traefik: %w", err)
	}

	logger.Info("successfully deleted traefik resources", "cluster", clusterName)

	return nil
}

// ForceDelete signals the [Actuator] to delete any resources managed by it,
// because of a force-delete event of the shoot cluster. This method implements
// the [extension.Actuator] interface.
//
// During force-delete the shoot API server is unreachable, so we set
// keepObjects=true on the shoot ManagedResource to let resource-manager remove
// its finalizer immediately without trying to reach the shoot. The seed DNS
// record is still cleaned up normally.
func (a *Actuator) ForceDelete(ctx context.Context, logger logr.Logger, ex *extensionsv1alpha1.Extension) error {
	clusterName := ex.Namespace

	defer func() {
		metrics.ActuatorOperationTotal.WithLabelValues(clusterName, "force_delete").Inc()
	}()

	logger.Info("shoot has been force-deleted, deleting traefik resources", "cluster", clusterName)

	deployer := traefik.NewDeployer(a.client, logger, traefik.DefaultConfig(), a.imageVector)

	// Best-effort deletion of the DNS record; the shoot is going away anyway.
	if err := deployer.DeleteDNSRecord(ctx, clusterName); err != nil {
		logger.Error(err, "failed to delete traefik ingress DNS record during force-delete", "cluster", clusterName)
	}

	// Delete shoot ManagedResource with keepObjects=true because the shoot
	// API server is unreachable during force-delete.
	if err := deployer.Delete(ctx, clusterName, true); err != nil {
		return fmt.Errorf("failed to force-delete traefik: %w", err)
	}

	return nil
}

// Restore restores the resources managed by the extension [Actuator]. This
// method implements the [extension.Actuator] interface.
func (a *Actuator) Restore(ctx context.Context, logger logr.Logger, ex *extensionsv1alpha1.Extension) error {
	// Increment our example metrics counter
	defer func() {
		metrics.ActuatorOperationTotal.WithLabelValues(ex.Namespace, "restore").Inc()
	}()

	return a.Reconcile(ctx, logger, ex)
}

// Migrate signals the [Actuator] to clean up control-plane resources managed
// by it, because of a shoot control-plane migration event. This method
// implements the [extension.Actuator] interface.
//
// During migration, shoot objects must be preserved (keepObjects=true) while
// the seed-side ManagedResources are deleted, so that the new seed can
// re-create them.
func (a *Actuator) Migrate(ctx context.Context, logger logr.Logger, ex *extensionsv1alpha1.Extension) error {
	clusterName := ex.Namespace

	defer func() {
		metrics.ActuatorOperationTotal.WithLabelValues(clusterName, "migrate").Inc()
	}()

	logger.Info("migrating traefik extension, cleaning up control-plane resources", "cluster", clusterName)

	deployer := traefik.NewDeployer(a.client, logger, traefik.DefaultConfig(), a.imageVector)

	// Delete the seed DNSRecord ManagedResource; the new seed will recreate it.
	if err := deployer.DeleteDNSRecord(ctx, clusterName); err != nil {
		return fmt.Errorf("failed to delete traefik ingress DNS record during migrate: %w", err)
	}

	// Keep shoot objects alive (traefik keeps running in the shoot) and only
	// remove the ManagedResource from the old seed.
	if err := deployer.Delete(ctx, clusterName, true); err != nil {
		return fmt.Errorf("failed to delete traefik managed resource during migrate: %w", err)
	}

	logger.Info("successfully migrated traefik extension", "cluster", clusterName)

	return nil
}
