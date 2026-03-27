// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	authenticationv1alpha1 "github.com/gardener/gardener/pkg/apis/authentication/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// whoamiImage is the test server used for ingress validation.
	// traefik/whoami is a tiny Go HTTP server that returns OS information and HTTP request details.
	whoamiImage = "traefik/whoami:v1.10.3"
	// whoamiPort is the HTTP port exposed by the whoami container.
	whoamiPort = 80
	// testNamespace is the namespace where test workloads are deployed on the shoot cluster.
	testNamespace = "default"
)

var _ = Describe("Traefik Extension E2E", Ordered, func() {
	var (
		shootKubernetesIngress      *gardencorev1beta1.Shoot
		shootKubernetesIngressNGINX *gardencorev1beta1.Shoot
	)

	BeforeAll(func() {
		shootKubernetesIngress = newShootObject(
			shootName(shootBaseName, "ki"),
			"KubernetesIngress",
		)
		shootKubernetesIngressNGINX = newShootObject(
			shootName(shootBaseName, "nx"),
			"KubernetesIngressNGINX",
		)
	})

	AfterAll(func() {
		// Cleanup shoots regardless of test outcome.
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), ShootDeletionTimeout)
		defer cleanupCancel()

		By("Cleaning up shoot clusters")
		deleteShoot(cleanupCtx, shootKubernetesIngress)
		deleteShoot(cleanupCtx, shootKubernetesIngressNGINX)

		By("Waiting for shoot deletion to complete")
		waitForShootDeletion(cleanupCtx, shootKubernetesIngress)
		waitForShootDeletion(cleanupCtx, shootKubernetesIngressNGINX)
	})

	Context("KubernetesIngress provider", func() {
		It("should create a shoot with KubernetesIngress provider", func() {
			createShootAndWaitForReady(ctx, shootKubernetesIngress)
		})

		It("should deploy a test workload and validate ingress connectivity", func() {
			shootClient := getShootClient(ctx, shootKubernetesIngress)
			verifyIngress(ctx, shootClient, "traefik")
		})
	})

	Context("KubernetesIngressNGINX provider", func() {
		It("should create a shoot with KubernetesIngressNGINX provider", func() {
			createShootAndWaitForReady(ctx, shootKubernetesIngressNGINX)
		})

		It("should deploy a test workload and validate ingress connectivity", func() {
			shootClient := getShootClient(ctx, shootKubernetesIngressNGINX)
			verifyIngress(ctx, shootClient, "nginx")
		})
	})
})

// newShootObject creates a Shoot manifest configured with the traefik extension.
func newShootObject(name, ingressProvider string) *gardencorev1beta1.Shoot {
	shoot := &gardencorev1beta1.Shoot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: projectNamespace,
			Annotations: map[string]string{
				"shoot.gardener.cloud/cloud-config-execution-max-delay-seconds": "0",
			},
		},
		Spec: gardencorev1beta1.ShootSpec{
			Purpose: purposePtr(gardencorev1beta1.ShootPurposeEvaluation),
			CloudProfile: &gardencorev1beta1.CloudProfileReference{
				Name: cloudProfileName,
				Kind: cloudProfileKind,
			},
			CredentialsBindingName: ptr.To(credentialsBinding),
			Region:                 region,
			Networking: &gardencorev1beta1.Networking{
				Type:  ptr.To(networkingType),
				Nodes: ptr.To(nodesCIDR),
			},
			Provider: gardencorev1beta1.Provider{
				Type: providerType,
				Workers: []gardencorev1beta1.Worker{
					{
						Name: "worker",
						Machine: gardencorev1beta1.Machine{
							Type: workerMachineType,
						},
						CRI: &gardencorev1beta1.CRI{
							Name: gardencorev1beta1.CRIName(workerCRIName),
						},
						Minimum: 1,
						Maximum: 2,
						Zones:   workerZones(),
					},
				},
			},
			Extensions: []gardencorev1beta1.Extension{
				{
					Type: "traefik",
					ProviderConfig: &runtime.RawExtension{
						Raw: mustMarshalJSON(map[string]interface{}{
							"apiVersion": "traefik.extensions.gardener.cloud/v1alpha1",
							"kind":       "TraefikConfig",
							"spec": map[string]interface{}{
								"replicas":        2,
								"ingressProvider": ingressProvider,
							},
						}),
					},
				},
			},
		},
	}

	// Apply optional volume settings.
	if workerVolumeSize != "" {
		shoot.Spec.Provider.Workers[0].Volume = &gardencorev1beta1.Volume{
			VolumeSize: workerVolumeSize,
		}
		if workerVolumeType != "" {
			shoot.Spec.Provider.Workers[0].Volume.Type = ptr.To(workerVolumeType)
		}
	}

	// Apply Kubernetes version if specified.
	if kubernetesVersion != "" {
		shoot.Spec.Kubernetes = gardencorev1beta1.Kubernetes{
			Version: kubernetesVersion,
		}
	}

	// Apply provider-specific infrastructure config.
	// If not explicitly set via INFRASTRUCTURE_CONFIG, generate a minimal default
	// for known providers that require it.
	if infrastructureConfig != "" {
		shoot.Spec.Provider.InfrastructureConfig = &runtime.RawExtension{
			Raw: []byte(infrastructureConfig),
		}
	} else if providerType == "aws" {
		infra := map[string]interface{}{
			"apiVersion": "aws.provider.extensions.gardener.cloud/v1alpha1",
			"kind":       "InfrastructureConfig",
			"networks": map[string]interface{}{
				"vpc": map[string]interface{}{
					"cidr": vpcCIDR,
				},
			},
		}
		// AWS requires at least one zone with subnet CIDRs when a zone is given.
		if workerZone != "" {
			networks := infra["networks"].(map[string]interface{})
			networks["zones"] = []map[string]interface{}{
				{
					"name":     workerZone,
					"workers":  workerZoneWorkersCIDR,
					"public":   workerZonePublicCIDR,
					"internal": workerZoneInternalCIDR,
				},
			}
		}
		shoot.Spec.Provider.InfrastructureConfig = &runtime.RawExtension{
			Raw: mustMarshalJSON(infra),
		}
		// AWS also requires a ControlPlaneConfig.
		shoot.Spec.Provider.ControlPlaneConfig = &runtime.RawExtension{
			Raw: mustMarshalJSON(map[string]interface{}{
				"apiVersion": "aws.provider.extensions.gardener.cloud/v1alpha1",
				"kind":       "ControlPlaneConfig",
				"loadBalancerController": map[string]interface{}{
					"enabled": false,
				},
			}),
		}
	}

	// Apply optional provider-specific worker config.
	if workersConfig != "" {
		shoot.Spec.Provider.Workers[0].ProviderConfig = &runtime.RawExtension{
			Raw: []byte(workersConfig),
		}
	}

	// Apply DNS domain if specified.
	if shootDomain != "" {
		shoot.Spec.DNS = &gardencorev1beta1.DNS{
			Domain: ptr.To(fmt.Sprintf("%s.%s", name, shootDomain)),
		}
	}

	return shoot
}

// createShootAndWaitForReady creates the shoot and waits until it reaches a healthy state.
func createShootAndWaitForReady(ctx context.Context, shoot *gardencorev1beta1.Shoot) {
	By(fmt.Sprintf("Creating shoot %s/%s", shoot.Namespace, shoot.Name))

	err := gardenClient.Create(ctx, shoot)
	if errors.IsAlreadyExists(err) {
		GinkgoWriter.Printf("Shoot %s/%s already exists, waiting for it to become ready\n", shoot.Namespace, shoot.Name)
	} else {
		Expect(err).NotTo(HaveOccurred(), "failed to create shoot")
	}

	By(fmt.Sprintf("Waiting for shoot %s to become ready (timeout: %s)", shoot.Name, ShootCreationTimeout))
	Eventually(func(g Gomega) {
		current := &gardencorev1beta1.Shoot{}
		g.Expect(gardenClient.Get(ctx, client.ObjectKeyFromObject(shoot), current)).To(Succeed())

		// Check that the last operation succeeded.
		g.Expect(current.Status.LastOperation).NotTo(BeNil(), "shoot has no last operation yet")
		GinkgoWriter.Printf("  Shoot %s: operation=%s state=%s progress=%d%%\n",
			shoot.Name,
			current.Status.LastOperation.Type,
			current.Status.LastOperation.State,
			current.Status.LastOperation.Progress,
		)
		g.Expect(current.Status.LastOperation.State).To(Equal(gardencorev1beta1.LastOperationStateSucceeded),
			fmt.Sprintf("shoot %s last operation not succeeded: %s - %s",
				shoot.Name,
				current.Status.LastOperation.State,
				current.Status.LastOperation.Description,
			),
		)

		// Update the local shoot object with the current status.
		shoot.Status = current.Status
		shoot.ResourceVersion = current.ResourceVersion
	}, ShootCreationTimeout, PollInterval).Should(Succeed(), "shoot %s did not become ready in time", shoot.Name)
}

// deleteShoot triggers deletion of a shoot by annotating it with the deletion confirmation
// and then deleting it.
func deleteShoot(ctx context.Context, shoot *gardencorev1beta1.Shoot) {
	current := &gardencorev1beta1.Shoot{}
	err := gardenClient.Get(ctx, client.ObjectKeyFromObject(shoot), current)
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())

	By(fmt.Sprintf("Annotating shoot %s for deletion confirmation", shoot.Name))
	patch := client.MergeFrom(current.DeepCopy())
	if current.Annotations == nil {
		current.Annotations = make(map[string]string)
	}
	current.Annotations["confirmation.gardener.cloud/deletion"] = "true"
	Expect(gardenClient.Patch(ctx, current, patch)).To(Succeed())

	By(fmt.Sprintf("Deleting shoot %s/%s", shoot.Namespace, shoot.Name))
	Expect(client.IgnoreNotFound(gardenClient.Delete(ctx, current))).To(Succeed())
}

// waitForShootDeletion waits until the shoot is fully deleted.
func waitForShootDeletion(ctx context.Context, shoot *gardencorev1beta1.Shoot) {
	Eventually(func(g Gomega) {
		current := &gardencorev1beta1.Shoot{}
		err := gardenClient.Get(ctx, client.ObjectKeyFromObject(shoot), current)
		g.Expect(errors.IsNotFound(err)).To(BeTrue(),
			fmt.Sprintf("shoot %s still exists (phase=%v)", shoot.Name, current.Status.LastOperation),
		)
	}, ShootDeletionTimeout, PollInterval).Should(Succeed(), "shoot %s was not deleted in time", shoot.Name)
}

// getShootClient creates a controller-runtime client for the shoot cluster by requesting
// an admin kubeconfig via the Gardener adminkubeconfig subresource.
func getShootClient(ctx context.Context, shoot *gardencorev1beta1.Shoot) client.Client {
	By(fmt.Sprintf("Requesting admin kubeconfig for shoot %s", shoot.Name))

	adminKubeconfigRequest := &authenticationv1alpha1.AdminKubeconfigRequest{
		Spec: authenticationv1alpha1.AdminKubeconfigRequestSpec{
			ExpirationSeconds: ptr.To[int64](7200),
		},
	}
	Expect(gardenClient.SubResource("adminkubeconfig").Create(ctx, shoot, adminKubeconfigRequest)).
		To(Succeed(), "failed to request admin kubeconfig for shoot %s", shoot.Name)

	kubeconfigData := adminKubeconfigRequest.Status.Kubeconfig
	Expect(kubeconfigData).NotTo(BeEmpty(), "admin kubeconfig is empty for shoot %s", shoot.Name)

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	Expect(err).NotTo(HaveOccurred(), "failed to build REST config from shoot kubeconfig")

	shootScheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(shootScheme)).To(Succeed())

	shootClient, err := client.New(restConfig, client.Options{Scheme: shootScheme})
	Expect(err).NotTo(HaveOccurred(), "failed to create shoot client")

	By(fmt.Sprintf("Waiting for shoot %s API server to become reachable", shoot.Name))
	waitForShootAPIReachable(ctx, shootClient, shoot)

	return shootClient
}

// waitForShootAPIReachable waits until a basic API call to the shoot cluster succeeds.
// Gardener may mark a shoot as ready before the API server is reachable from the test runner
// (e.g. due to DNS propagation delays or transient network conditions).
func waitForShootAPIReachable(ctx context.Context, shootClient client.Client, shoot *gardencorev1beta1.Shoot) {
	Eventually(func(g Gomega) {
		nsList := &corev1.NamespaceList{}
		g.Expect(shootClient.List(ctx, nsList)).To(Succeed())
	}, DeploymentReadyTimeout, PollInterval).Should(Succeed(),
		"shoot %s API server did not become reachable in time", shoot.Name)
}

// verifyIngress deploys a whoami test workload, creates an Ingress, and validates HTTP connectivity.
func verifyIngress(ctx context.Context, shootClient client.Client, ingressClassName string) {
	appName := "whoami"
	labels := map[string]string{"app": appName}

	By("Deploying whoami test workload")
	deployWhoami(ctx, shootClient, appName, labels)

	By("Creating Ingress resource")
	ingress := createIngress(ctx, shootClient, appName, ingressClassName)

	By("Waiting for the ingress controller to admit the Ingress (status populated)")
	waitForIngressAdmission(ctx, shootClient, ingress)

	By("Waiting for the Traefik LoadBalancer to get an external address")
	lbAddress := waitForTraefikLBAddress(ctx, shootClient)
	GinkgoWriter.Printf("Traefik LoadBalancer address: %s\n", lbAddress)

	By("Validating HTTP connectivity through Traefik ingress")
	validateHTTPConnectivity(ctx, lbAddress)
}

// deployWhoami creates a Deployment and Service for the whoami test container.
func deployWhoami(ctx context.Context, shootClient client.Client, name string, labels map[string]string) {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To[int32](1),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  name,
							Image: whoamiImage,
							Ports: []corev1.ContainerPort{
								{ContainerPort: whoamiPort, Protocol: corev1.ProtocolTCP},
							},
						},
					},
				},
			},
		},
	}
	Expect(shootClient.Create(ctx, deployment)).To(Succeed(), "failed to create whoami deployment")

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: labels,
			Ports: []corev1.ServicePort{
				{
					Port:       whoamiPort,
					TargetPort: intstr.FromInt32(whoamiPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
	Expect(shootClient.Create(ctx, service)).To(Succeed(), "failed to create whoami service")

	// Wait for the deployment to be ready.
	Eventually(func(g Gomega) {
		dep := &appsv1.Deployment{}
		g.Expect(shootClient.Get(ctx, client.ObjectKeyFromObject(deployment), dep)).To(Succeed())
		g.Expect(dep.Status.ReadyReplicas).To(Equal(*dep.Spec.Replicas),
			fmt.Sprintf("whoami deployment not ready: %d/%d replicas", dep.Status.ReadyReplicas, *dep.Spec.Replicas),
		)
	}, DeploymentReadyTimeout, PollInterval).Should(Succeed(), "whoami deployment did not become ready")
}

// createIngress creates an Ingress resource pointing to the whoami service and returns it.
func createIngress(ctx context.Context, shootClient client.Client, serviceName, ingressClassName string) *networkingv1.Ingress {
	pathType := networkingv1.PathTypePrefix

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: testNamespace,
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ptr.To(ingressClassName),
			Rules: []networkingv1.IngressRule{
				{
					// Use a wildcard / empty host so the test works with plain IP access.
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: serviceName,
											Port: networkingv1.ServiceBackendPort{
												Number: whoamiPort,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	Expect(shootClient.Create(ctx, ingress)).To(Succeed(), "failed to create ingress")
	return ingress
}

// waitForIngressAdmission waits until the ingress controller has admitted the Ingress, which is
// signalled by the controller writing an address into ingress.status.loadBalancer.ingress.
// This proves that the specific ingress controller (traefik or nginx) has actually processed
// the routing rule, not merely that the LB Service is reachable.
func waitForIngressAdmission(ctx context.Context, shootClient client.Client, ingress *networkingv1.Ingress) {
	Eventually(func(g Gomega) {
		current := &networkingv1.Ingress{}
		g.Expect(shootClient.Get(ctx, client.ObjectKeyFromObject(ingress), current)).To(Succeed())
		g.Expect(current.Status.LoadBalancer.Ingress).NotTo(BeEmpty(),
			"ingress controller has not yet admitted the Ingress (status.loadBalancer.ingress is empty)",
		)
	}, IngressReadyTimeout, PollInterval).Should(Succeed(), "ingress %s was not admitted in time", ingress.Name)
}

// waitForTraefikLBAddress polls the Traefik Service in kube-system until it gets an external LB address.
func waitForTraefikLBAddress(ctx context.Context, shootClient client.Client) string {
	var address string
	Eventually(func(g Gomega) {
		svc := &corev1.Service{}
		g.Expect(shootClient.Get(ctx, types.NamespacedName{
			Namespace: "kube-system",
			Name:      "traefik",
		}, svc)).To(Succeed())

		g.Expect(svc.Status.LoadBalancer.Ingress).NotTo(BeEmpty(), "traefik service has no LB ingress yet")

		ing := svc.Status.LoadBalancer.Ingress[0]
		if ing.IP != "" {
			address = ing.IP
		} else {
			address = ing.Hostname
		}
		g.Expect(address).NotTo(BeEmpty(), "traefik LB address is empty")
	}, IngressReadyTimeout, PollInterval).Should(Succeed(), "traefik LB address not available in time")

	return address
}

// validateHTTPConnectivity sends HTTP requests to the whoami service through the ingress
// and validates the response.
func validateHTTPConnectivity(ctx context.Context, lbAddress string) {
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // e2e test with self-signed certs
			},
		},
	}

	url := fmt.Sprintf("http://%s/", lbAddress)

	Eventually(func(g Gomega) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		g.Expect(err).NotTo(HaveOccurred())

		resp, err := httpClient.Do(req)
		g.Expect(err).NotTo(HaveOccurred(), "HTTP request to whoami through traefik failed")
		defer resp.Body.Close()

		g.Expect(resp.StatusCode).To(Equal(http.StatusOK),
			fmt.Sprintf("expected HTTP 200, got %d", resp.StatusCode),
		)

		// Read the response body and verify it contains whoami output.
		buf := make([]byte, 4096)
		n, _ := resp.Body.Read(buf)
		body := string(buf[:n])

		// whoami returns headers like "Hostname: <pod-name>" and request info.
		g.Expect(body).To(SatisfyAny(
			ContainSubstring("Hostname"),
			ContainSubstring("hostname"),
			ContainSubstring("Name"),
		), "response body does not look like whoami output: %s", body)

		GinkgoWriter.Printf("Successfully reached whoami via traefik ingress at %s\n", url)
		GinkgoWriter.Printf("Response body:\n%s\n", strings.TrimSpace(body))
	}, IngressReadyTimeout, 15*time.Second).Should(Succeed(), "could not reach whoami through traefik ingress")
}

// shootName builds a shoot name from base+suffix, auto-truncating the base so that
// len(projectName) + len(shootName) <= 21 (Gardener's hard limit).
// The project name is derived from the project namespace by trimming the "garden-" prefix.
func shootName(base, suffix string) string {
	projectName := strings.TrimPrefix(projectNamespace, "garden-")
	// maxShoot = 21 - len(project), minus 1 for the "-" separator, minus len(suffix)
	maxBase := 21 - len(projectName) - 1 - len(suffix)
	if maxBase < 1 {
		maxBase = 1
	}
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	base = strings.TrimRight(base, "-")
	return fmt.Sprintf("%s-%s", base, suffix)
}

// workerZones returns the zones to set on the worker spec.
// Priority: WORKER_SPEC_ZONES > WORKER_ZONE > nil.
// Set WORKER_SPEC_ZONES to a comma-separated list to override.
// Set WORKER_ZONE only to use a single zone for both InfrastructureConfig and the worker spec.
func workerZones() []string {
	if workerSpecZones != "" {
		return strings.Split(workerSpecZones, ",")
	}
	if workerZone != "" {
		return []string{workerZone}
	}
	return nil
}

func purposePtr(p gardencorev1beta1.ShootPurpose) *gardencorev1beta1.ShootPurpose {
	return &p
}

func mustMarshalJSON(v interface{}) []byte {
	data, err := json.Marshal(v)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	return data
}
