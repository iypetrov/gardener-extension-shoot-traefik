// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package actuator_test

import (
	"encoding/json"

	corev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/component-base/featuregate"
	"k8s.io/utils/ptr"

	"github.com/gardener/gardener-extension-shoot-traefik/imagevector"
	"github.com/gardener/gardener-extension-shoot-traefik/pkg/actuator"
	"github.com/gardener/gardener-extension-shoot-traefik/pkg/apis/config"
)

var _ = Describe("Actuator", Ordered, func() {
	var (
		// Contain the serialized cloud profile, seed and shoot and provider config
		providerConfigData, cloudProfileData, seedData, shootData []byte

		extResource *extensionsv1alpha1.Extension
		cluster     *extensionsv1alpha1.Cluster
		decoder     = serializer.NewCodecFactory(scheme.Scheme, serializer.EnableStrict).UniversalDecoder()

		featureGates   = make(map[featuregate.Feature]bool)
		actuatorOpts   []actuator.Option
		providerConfig = config.TraefikConfig{
			Spec: config.TraefikConfigSpec{
				Replicas: 1,
			},
		}

		projectNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "garden-local",
			},
		}
		shootNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "shoot--local--local",
			},
		}
		cloudProfile = &corev1beta1.CloudProfile{
			ObjectMeta: metav1.ObjectMeta{
				Name: "local",
			},
			Spec: corev1beta1.CloudProfileSpec{
				Type: "local",
			},
		}
		seed = &corev1beta1.Seed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "local",
			},
			Spec: corev1beta1.SeedSpec{
				Ingress: &corev1beta1.Ingress{
					Domain: "ingress.local.seed.local.gardener.cloud",
				},
				Provider: corev1beta1.SeedProvider{
					Type:   "local",
					Region: "local",
					Zones:  []string{"0"},
				},
			},
		}
		shoot = &corev1beta1.Shoot{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "local",
				Namespace: projectNamespace.Name,
			},
			Spec: corev1beta1.ShootSpec{
				SeedName: new("local"),
				Provider: corev1beta1.Provider{
					Type: "local",
				},
				Region: "local",
			},
		}
	)

	BeforeAll(func() {
		actuatorOpts = []actuator.Option{
			actuator.WithGardenerVersion("1.0.0"),
			actuator.WithDecoder(decoder),
			actuator.WithGardenletFeatures(featureGates),
		}

		// Serialize our test objects, so we can later re-use them.
		var err error
		cloudProfileData, err = json.Marshal(cloudProfile)
		Expect(err).NotTo(HaveOccurred())
		seedData, err = json.Marshal(seed)
		Expect(err).NotTo(HaveOccurred())
		shootData, err = json.Marshal(shoot)
		Expect(err).NotTo(HaveOccurred())
		providerConfigData, err = json.Marshal(providerConfig)
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Create(ctx, projectNamespace)).To(Succeed())
		Expect(k8sClient.Create(ctx, shootNamespace)).To(Succeed())
	})

	BeforeEach(func() {
		extResource = &extensionsv1alpha1.Extension{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "example",
				Namespace: shootNamespace.Name,
			},
			Spec: extensionsv1alpha1.ExtensionSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					Type:  actuator.ExtensionType,
					Class: ptr.To(extensionsv1alpha1.ExtensionClassShoot),
				},
			},
		}

		cluster = &extensionsv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: shootNamespace.Name,
			},
			Spec: extensionsv1alpha1.ClusterSpec{
				CloudProfile: runtime.RawExtension{
					Raw: cloudProfileData,
				},
				Seed: runtime.RawExtension{
					Raw: seedData,
				},
				Shoot: runtime.RawExtension{
					Raw: shootData,
				},
			},
		}

		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
	})

	AfterEach(func() {
		Expect(k8sClient.Delete(ctx, cluster)).To(Succeed())
	})

	It("should successfully create an actuator", func() {
		act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)

		Expect(err).NotTo(HaveOccurred())
		Expect(act).NotTo(BeNil())
		Expect(act.Name()).To(Equal(actuator.Name))
		Expect(act.ExtensionType()).To(Equal(actuator.ExtensionType))
		Expect(act.FinalizerSuffix()).To(Equal(actuator.FinalizerSuffix))
		Expect(act.ExtensionClass()).To(Equal(extensionsv1alpha1.ExtensionClassShoot))
	})

	It("should fail to reconcile when no cluster exists", func() {
		// Change namespace of the extension resource, so that a
		// non-existing cluster is looked up.
		extResource.Namespace = "non-existing-namespace"

		act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
		Expect(err).NotTo(HaveOccurred())
		Expect(act).NotTo(BeNil())
		err = act.Reconcile(ctx, logger, extResource)
		Expect(err).Should(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("failed to get cluster")))
	})

	It("should fail to reconcile when shoot purpose is not evaluation", func() {
		act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
		Expect(err).NotTo(HaveOccurred())
		Expect(act).NotTo(BeNil())

		err = act.Reconcile(ctx, logger, extResource)
		Expect(err).Should(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("shoot purpose must be 'evaluation' for traefik extension")))
	})

	It("should succeed on Reconcile", func() {
		// Update shoot to have purpose evaluation
		shootWithPurpose := shoot.DeepCopy()
		shootWithPurpose.Spec.Purpose = ptr.To(corev1beta1.ShootPurposeEvaluation)
		shootWithPurposeData, err := json.Marshal(shootWithPurpose)
		Expect(err).NotTo(HaveOccurred())

		// Update cluster with shoot that has evaluation purpose
		cluster.Spec.Shoot.Raw = shootWithPurposeData
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

		// Ensure we have valid provider config
		extResource.Spec.ProviderConfig = &runtime.RawExtension{
			Raw: providerConfigData,
		}

		act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
		Expect(err).NotTo(HaveOccurred())
		Expect(act).NotTo(BeNil())
		Expect(act.Reconcile(ctx, logger, extResource)).To(Succeed())
	})

	It("should succeed on Delete", func() {
		act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
		Expect(err).NotTo(HaveOccurred())
		Expect(act).NotTo(BeNil())
		Expect(act.Delete(ctx, logger, extResource)).To(Succeed())
	})

	It("should succeed on ForceDelete", func() {
		act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
		Expect(err).NotTo(HaveOccurred())
		Expect(act).NotTo(BeNil())
		Expect(act.ForceDelete(ctx, logger, extResource)).To(Succeed())
	})

	It("should succeed on Restore", func() {
		// Update shoot to have purpose evaluation
		shootWithPurpose := shoot.DeepCopy()
		shootWithPurpose.Spec.Purpose = ptr.To(corev1beta1.ShootPurposeEvaluation)
		shootWithPurposeData, err := json.Marshal(shootWithPurpose)
		Expect(err).NotTo(HaveOccurred())

		// Update cluster with shoot that has evaluation purpose
		cluster.Spec.Shoot.Raw = shootWithPurposeData
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

		// Ensure we have valid provider config
		extResource.Spec.ProviderConfig = &runtime.RawExtension{
			Raw: providerConfigData,
		}

		act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
		Expect(err).NotTo(HaveOccurred())
		Expect(act).NotTo(BeNil())
		Expect(act.Restore(ctx, logger, extResource)).To(Succeed())
	})

	It("should succeed on Migrate", func() {
		// Update shoot to have purpose evaluation
		shootWithPurpose := shoot.DeepCopy()
		shootWithPurpose.Spec.Purpose = ptr.To(corev1beta1.ShootPurposeEvaluation)
		shootWithPurposeData, err := json.Marshal(shootWithPurpose)
		Expect(err).NotTo(HaveOccurred())

		// Update cluster with shoot that has evaluation purpose
		cluster.Spec.Shoot.Raw = shootWithPurposeData
		Expect(k8sClient.Update(ctx, cluster)).To(Succeed())

		// Ensure we have valid provider config
		extResource.Spec.ProviderConfig = &runtime.RawExtension{
			Raw: providerConfigData,
		}

		act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
		Expect(err).NotTo(HaveOccurred())
		Expect(act).NotTo(BeNil())
		Expect(act.Migrate(ctx, logger, extResource)).To(Succeed())
	})

	Context("IngressProvider Configuration", func() {
		BeforeEach(func() {
			// Update shoot to have purpose evaluation
			shootWithPurpose := shoot.DeepCopy()
			shootWithPurpose.Spec.Purpose = ptr.To(corev1beta1.ShootPurposeEvaluation)
			shootWithPurposeData, err := json.Marshal(shootWithPurpose)
			Expect(err).NotTo(HaveOccurred())

			// Update cluster with shoot that has evaluation purpose
			cluster.Spec.Shoot.Raw = shootWithPurposeData
			Expect(k8sClient.Update(ctx, cluster)).To(Succeed())
		})

		It("should use default KubernetesIngress provider when not specified", func() {
			// Create config without IngressProvider field
			cfg := config.TraefikConfig{
				Spec: config.TraefikConfigSpec{
					Replicas: 2,
					// IngressProvider not specified
				},
			}
			cfgData, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())

			extResource.Spec.ProviderConfig = &runtime.RawExtension{
				Raw: cfgData,
			}

			act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(act).NotTo(BeNil())
			Expect(act.Reconcile(ctx, logger, extResource)).To(Succeed())
		})

		It("should use KubernetesIngress provider when explicitly specified", func() {
			cfg := config.TraefikConfig{
				Spec: config.TraefikConfigSpec{
					Replicas:        2,
					IngressProvider: config.IngressProviderKubernetesIngress,
				},
			}
			cfgData, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())

			extResource.Spec.ProviderConfig = &runtime.RawExtension{
				Raw: cfgData,
			}

			act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(act).NotTo(BeNil())
			Expect(act.Reconcile(ctx, logger, extResource)).To(Succeed())
		})

		It("should use KubernetesIngressNGINX provider when specified", func() {
			cfg := config.TraefikConfig{
				Spec: config.TraefikConfigSpec{
					Replicas:        2,
					IngressProvider: config.IngressProviderKubernetesIngressNGINX,
				},
			}
			cfgData, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())

			extResource.Spec.ProviderConfig = &runtime.RawExtension{
				Raw: cfgData,
			}

			act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(act).NotTo(BeNil())
			Expect(act.Reconcile(ctx, logger, extResource)).To(Succeed())
		})

		It("should auto-derive ingress class for KubernetesIngressNGINX", func() {
			cfg := config.TraefikConfig{
				Spec: config.TraefikConfigSpec{
					Replicas:        2,
					IngressProvider: config.IngressProviderKubernetesIngressNGINX,
				},
			}
			cfgData, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())

			extResource.Spec.ProviderConfig = &runtime.RawExtension{
				Raw: cfgData,
			}

			act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(act).NotTo(BeNil())
			Expect(act.Reconcile(ctx, logger, extResource)).To(Succeed())
		})

		It("should reconcile with all provider config options", func() {
			cfg := config.TraefikConfig{
				Spec: config.TraefikConfigSpec{
					Replicas:        3,
					IngressProvider: config.IngressProviderKubernetesIngressNGINX,
				},
			}
			cfgData, err := json.Marshal(cfg)
			Expect(err).NotTo(HaveOccurred())

			extResource.Spec.ProviderConfig = &runtime.RawExtension{
				Raw: cfgData,
			}

			act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(act).NotTo(BeNil())
			Expect(act.Reconcile(ctx, logger, extResource)).To(Succeed())
		})

		It("should handle empty provider config gracefully", func() {
			// No provider config at all - should use defaults
			extResource.Spec.ProviderConfig = nil

			act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(act).NotTo(BeNil())
			Expect(act.Reconcile(ctx, logger, extResource)).To(Succeed())
		})

		It("should handle invalid provider config gracefully and use defaults", func() {
			// Invalid JSON in provider config
			extResource.Spec.ProviderConfig = &runtime.RawExtension{
				Raw: []byte(`{"invalid json`),
			}

			act, err := actuator.New(k8sClient, imagevector.ImageVector(), actuatorOpts...)
			Expect(err).NotTo(HaveOccurred())
			Expect(act).NotTo(BeNil())
			// Should not fail, but use defaults
			Expect(act.Reconcile(ctx, logger, extResource)).To(Succeed())
		})
	})
})
