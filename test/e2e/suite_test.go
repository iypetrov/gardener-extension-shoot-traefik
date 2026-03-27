// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package e2e_test

import (
	"context"
	"os"
	"testing"
	"time"

	authenticationv1alpha1 "github.com/gardener/gardener/pkg/apis/authentication/v1alpha1"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	// ShootCreationTimeout is the maximum time to wait for a shoot to be created and reconciled.
	ShootCreationTimeout = 30 * time.Minute
	// ShootDeletionTimeout is the maximum time to wait for a shoot to be deleted.
	ShootDeletionTimeout = 30 * time.Minute
	// IngressReadyTimeout is the maximum time to wait for an ingress to become reachable.
	IngressReadyTimeout = 10 * time.Minute
	// DeploymentReadyTimeout is the maximum time to wait for a deployment to become ready.
	DeploymentReadyTimeout = 5 * time.Minute
	// PollInterval is the interval between status checks.
	PollInterval = 10 * time.Second
)

var (
	ctx           context.Context
	cancel        context.CancelFunc
	gardenClient  client.Client
	gardenScheme  *runtime.Scheme
	shootBaseName string

	projectNamespace       string
	cloudProfileName       string
	cloudProfileKind       string
	credentialsBinding     string
	region                 string
	providerType           string
	networkingType         string
	workerMachineType      string
	workerCRIName          string
	kubernetesVersion      string
	nodesCIDR              string
	shootDomain            string
	workerVolumeSize       string
	workerVolumeType       string
	workerZone             string // availability zone for InfrastructureConfig network layout
	workerSpecZones        string // comma-separated zones for the worker spec; empty = let Gardener auto-schedule
	vpcCIDR                string
	workerZoneWorkersCIDR  string // subnet CIDR for worker nodes in the zone
	workerZonePublicCIDR   string // subnet CIDR for public traffic in the zone
	workerZoneInternalCIDR string // subnet CIDR for internal traffic in the zone
	infrastructureConfig   string
	workersConfig          string
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Test Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	projectNamespace = getEnvOrDefault("PROJECT_NAMESPACE", "garden-local")
	cloudProfileName = getEnvOrDefault("CLOUD_PROFILE_NAME", "local")
	cloudProfileKind = getEnvOrDefault("CLOUD_PROFILE_KIND", "CloudProfile")
	credentialsBinding = getEnvOrDefault("CREDENTIALS_BINDING", "local")
	region = getEnvOrDefault("REGION", "local")
	providerType = getEnvOrDefault("PROVIDER_TYPE", "local")
	networkingType = getEnvOrDefault("NETWORKING_TYPE", "calico")
	workerMachineType = getEnvOrDefault("WORKER_MACHINE_TYPE", "local")
	workerCRIName = getEnvOrDefault("WORKER_CRI_NAME", "containerd")
	kubernetesVersion = os.Getenv("KUBERNETES_VERSION")
	nodesCIDR = getEnvOrDefault("NODES_CIDR", "10.0.0.0/16")
	shootDomain = os.Getenv("SHOOT_DOMAIN")
	shootBaseName = getEnvOrDefault("SHOOT_BASE_NAME", "traefik-e2e")
	workerVolumeSize = os.Getenv("WORKER_VOLUME_SIZE")
	workerVolumeType = os.Getenv("WORKER_VOLUME_TYPE")
	workerZone = os.Getenv("WORKER_ZONE")
	workerSpecZones = os.Getenv("WORKER_SPEC_ZONES")
	vpcCIDR = getEnvOrDefault("VPC_CIDR", nodesCIDR)
	workerZoneWorkersCIDR = getEnvOrDefault("WORKER_ZONE_WORKERS_CIDR", "10.250.0.0/19")
	workerZonePublicCIDR = getEnvOrDefault("WORKER_ZONE_PUBLIC_CIDR", "10.250.96.0/22")
	workerZoneInternalCIDR = getEnvOrDefault("WORKER_ZONE_INTERNAL_CIDR", "10.250.112.0/22")
	infrastructureConfig = os.Getenv("INFRASTRUCTURE_CONFIG")
	workersConfig = os.Getenv("WORKERS_CONFIG")

	kubeconfig := os.Getenv("KUBECONFIG")
	Expect(kubeconfig).NotTo(BeEmpty(), "KUBECONFIG environment variable must be set")

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	Expect(err).NotTo(HaveOccurred(), "failed to build REST config from KUBECONFIG")

	gardenScheme = runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(gardenScheme)).To(Succeed())
	Expect(gardencorev1beta1.AddToScheme(gardenScheme)).To(Succeed())
	Expect(authenticationv1alpha1.AddToScheme(gardenScheme)).To(Succeed())

	gardenClient, err = client.New(restConfig, client.Options{Scheme: gardenScheme})
	Expect(err).NotTo(HaveOccurred(), "failed to create garden client")

	_, err = kubernetes.NewForConfig(restConfig)
	Expect(err).NotTo(HaveOccurred(), "failed to create kubernetes clientset")
})

var _ = AfterSuite(func() {
	cancel()
})

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
