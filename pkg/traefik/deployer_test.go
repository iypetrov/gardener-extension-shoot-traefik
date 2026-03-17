// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package traefik

import (
	"slices"
	"strings"
	"testing"

	"github.com/gardener/gardener/pkg/utils/imagevector"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/gardener/gardener-extension-shoot-traefik/pkg/apis/config"
)

func TestDeployment_ImageOverride(t *testing.T) {
	tests := []struct {
		name          string
		imageVector   imagevector.ImageVector
		expectedImage string
		expectError   bool
		errorContains string
	}{
		{
			name: "use image vector when config empty",
			imageVector: imagevector.ImageVector{
				{
					Name:       "traefik",
					Repository: new("docker.io/library/traefik"),
					Tag:        new("v3.6.10"),
				},
			},
			expectedImage: "docker.io/library/traefik:v3.6.10",
			expectError:   false,
		},
		{
			name:          "fail when config empty and image not in vector",
			imageVector:   imagevector.ImageVector{}, // Empty vector
			expectedImage: "",
			expectError:   true,
			errorContains: "failed to find traefik image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a fake client
			scheme := runtime.NewScheme()
			client := fake.NewClientBuilder().WithScheme(scheme).Build()

			config := Config{
				Replicas:     2,
				IngressClass: "traefik",
			}

			deployer := NewDeployer(client, logr.Discard(), config, tt.imageVector)

			deployment, err := deployer.deployment()

			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errorContains, err)
				}

				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)

				return
			}

			if deployment == nil {
				t.Error("expected deployment but got nil")

				return
			}

			actualImage := deployment.Spec.Template.Spec.Containers[0].Image
			if actualImage != tt.expectedImage {
				t.Errorf("expected image %q, got %q", tt.expectedImage, actualImage)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

func TestDeployment_IngressProvider(t *testing.T) {
	tests := []struct {
		name            string
		ingressProvider config.IngressProviderType
		ingressClass    string
		expectedArgs    []string
		notExpectedArgs []string
	}{
		{
			name:            "KubernetesIngress provider",
			ingressProvider: config.IngressProviderKubernetesIngress,
			ingressClass:    "traefik",
			expectedArgs: []string{
				"--providers.kubernetesingress=true",
				"--providers.kubernetesingress.ingressclass=traefik",
			},
			notExpectedArgs: []string{
				"--providers.kubernetesingressnginx",
			},
		},
		{
			name:            "KubernetesIngressNGINX provider",
			ingressProvider: config.IngressProviderKubernetesIngressNGINX,
			ingressClass:    "nginx",
			expectedArgs: []string{
				"--providers.kubernetesingressnginx=true",
				"--providers.kubernetesingressnginx.ingressclass=nginx",
			},
			notExpectedArgs: []string{
				"--providers.kubernetesingress=true",
				"--providers.kubernetesingress.ingressclass",
			},
		},
		{
			name:            "empty provider defaults to KubernetesIngress",
			ingressProvider: "",
			ingressClass:    "traefik",
			expectedArgs: []string{
				"--providers.kubernetesingress=true",
				"--providers.kubernetesingress.ingressclass=traefik",
			},
			notExpectedArgs: []string{
				"--providers.kubernetesingressnginx",
			},
		},
		{
			name:            "NGINX provider with custom class",
			ingressProvider: config.IngressProviderKubernetesIngressNGINX,
			ingressClass:    "custom-nginx",
			expectedArgs: []string{
				"--providers.kubernetesingressnginx=true",
				"--providers.kubernetesingressnginx.ingressclass=custom-nginx",
			},
			notExpectedArgs: []string{
				"--providers.kubernetesingress=true",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			client := fake.NewClientBuilder().WithScheme(scheme).Build()

			imageVec := imagevector.ImageVector{
				{
					Name:       "traefik",
					Repository: new("docker.io/library/traefik"),
					Tag:        new("v3.6.10"),
				},
			}

			config := Config{
				Replicas:        2,
				IngressClass:    tt.ingressClass,
				IngressProvider: tt.ingressProvider,
			}

			deployer := NewDeployer(client, logr.Discard(), config, imageVec)
			deployment, err := deployer.deployment()

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if deployment == nil {
				t.Fatal("expected deployment but got nil")
			}

			args := deployment.Spec.Template.Spec.Containers[0].Args

			// Check expected args are present
			for _, expectedArg := range tt.expectedArgs {
				if !slices.Contains(args, expectedArg) {
					t.Errorf("expected arg %q not found in deployment args: %v", expectedArg, args)
				}
			}

			// Check unexpected args are not present
			for _, notExpectedArg := range tt.notExpectedArgs {
				for _, arg := range args {
					if strings.Contains(arg, notExpectedArg) {
						t.Errorf("unexpected arg containing %q found in deployment args: %v", notExpectedArg, args)
					}
				}
			}

			// Verify common args are always present
			commonArgs := []string{
				"--api.insecure=false",
				"--ping=true",
				"--metrics.prometheus=true",
				"--entrypoints.web.address=:8000",
				"--entrypoints.websecure.address=:8443",
			}
			for _, commonArg := range commonArgs {
				if !slices.Contains(args, commonArg) {
					t.Errorf("common arg %q not found in deployment args: %v", commonArg, args)
				}
			}
		})
	}
}

func TestDeployment_LogLevel(t *testing.T) {
	tests := []struct {
		name        string
		logLevel    string
		expectedArg string
	}{
		{
			name:        "INFO log level",
			logLevel:    "INFO",
			expectedArg: "--log.level=INFO",
		},
		{
			name:        "DEBUG log level",
			logLevel:    "DEBUG",
			expectedArg: "--log.level=DEBUG",
		},
		{
			name:        "WARN log level",
			logLevel:    "WARN",
			expectedArg: "--log.level=WARN",
		},
		{
			name:        "ERROR log level",
			logLevel:    "ERROR",
			expectedArg: "--log.level=ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			client := fake.NewClientBuilder().WithScheme(scheme).Build()

			imageVec := imagevector.ImageVector{
				{
					Name:       "traefik",
					Repository: new("docker.io/library/traefik"),
					Tag:        new("v3.6.10"),
				},
			}

			config := Config{
				Replicas:        2,
				IngressClass:    "traefik",
				IngressProvider: config.IngressProviderKubernetesIngress,
				LogLevel:        tt.logLevel,
			}

			deployer := NewDeployer(client, logr.Discard(), config, imageVec)
			deployment, err := deployer.deployment()

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if deployment == nil {
				t.Fatal("expected deployment but got nil")
			}

			args := deployment.Spec.Template.Spec.Containers[0].Args

			// Check that the expected log level arg is present
			if !slices.Contains(args, tt.expectedArg) {
				t.Errorf("expected arg %q not found in deployment args: %v", tt.expectedArg, args)
			}
		})
	}
}

func TestClusterRole_RBAC_Permissions(t *testing.T) {
	tests := []struct {
		name                 string
		ingressProvider      config.IngressProviderType
		expectNamespacePerms bool
	}{
		{
			name:                 "KubernetesIngress provider - no namespace permissions",
			ingressProvider:      config.IngressProviderKubernetesIngress,
			expectNamespacePerms: false,
		},
		{
			name:                 "KubernetesIngressNGINX provider - includes namespace permissions",
			ingressProvider:      config.IngressProviderKubernetesIngressNGINX,
			expectNamespacePerms: true,
		},
		{
			name:                 "empty provider defaults to KubernetesIngress - no namespace permissions",
			ingressProvider:      "",
			expectNamespacePerms: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			client := fake.NewClientBuilder().WithScheme(scheme).Build()

			config := Config{
				Replicas:        2,
				IngressClass:    "traefik",
				IngressProvider: tt.ingressProvider,
			}

			deployer := NewDeployer(client, logr.Discard(), config, nil)
			clusterRole := deployer.clusterRole()

			if clusterRole == nil {
				t.Fatal("expected cluster role but got nil")
			}

			// Check for namespace permissions
			hasNamespacePerms := false
			for _, rule := range clusterRole.Rules {
				if slices.Contains(rule.Resources, "namespaces") {
					hasNamespacePerms = true
					// Verify the permissions are correct
					expectedVerbs := []string{"get", "list", "watch"}
					for _, verb := range expectedVerbs {
						if !slices.Contains(rule.Verbs, verb) {
							t.Errorf("expected verb %q for namespaces resource not found", verb)
						}
					}

					break
				}
			}

			if tt.expectNamespacePerms && !hasNamespacePerms {
				t.Error("expected namespace permissions but they were not found")
			}
			if !tt.expectNamespacePerms && hasNamespacePerms {
				t.Error("unexpected namespace permissions found")
			}

			// Verify common permissions are always present
			commonResources := map[string][]string{
				"services":       {"get", "list", "watch"},
				"endpoints":      {"get", "list", "watch"},
				"secrets":        {"get", "list", "watch"},
				"ingresses":      {"get", "list", "watch"},
				"ingressclasses": {"get", "list", "watch"},
			}

			for resource, expectedVerbs := range commonResources {
				found := false
				for _, rule := range clusterRole.Rules {
					if slices.Contains(rule.Resources, resource) {
						found = true
						for _, verb := range expectedVerbs {
							if !slices.Contains(rule.Verbs, verb) {
								t.Errorf("expected verb %q for resource %q not found", verb, resource)
							}
						}

						break
					}
				}
				if !found {
					t.Errorf("expected resource %q not found in cluster role", resource)
				}
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	defaultCfg := DefaultConfig()
	if defaultCfg.Replicas != 2 {
		t.Errorf("expected default replicas to be 2, got %d", defaultCfg.Replicas)
	}

	if defaultCfg.IngressClass != "traefik" {
		t.Errorf("expected default ingress class to be 'traefik', got %q", defaultCfg.IngressClass)
	}

	if defaultCfg.IngressProvider != config.IngressProviderKubernetesIngress {
		t.Errorf("expected default ingress provider to be 'KubernetesIngress', got %q", defaultCfg.IngressProvider)
	}
}

func TestIngressClass_Controller(t *testing.T) {
	tests := []struct {
		name               string
		ingressProvider    config.IngressProviderType
		ingressClass       string
		expectedController string
	}{
		{
			name:               "KubernetesIngress provider - traefik controller",
			ingressProvider:    config.IngressProviderKubernetesIngress,
			ingressClass:       "traefik",
			expectedController: "traefik.io/ingress-controller",
		},
		{
			name:               "KubernetesIngressNGINX provider - nginx controller",
			ingressProvider:    config.IngressProviderKubernetesIngressNGINX,
			ingressClass:       "nginx",
			expectedController: "k8s.io/ingress-nginx",
		},
		{
			name:               "empty provider defaults to traefik controller",
			ingressProvider:    "",
			ingressClass:       "traefik",
			expectedController: "traefik.io/ingress-controller",
		},
		{
			name:               "NGINX provider with custom class name",
			ingressProvider:    config.IngressProviderKubernetesIngressNGINX,
			ingressClass:       "custom-nginx",
			expectedController: "k8s.io/ingress-nginx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			client := fake.NewClientBuilder().WithScheme(scheme).Build()

			config := Config{
				Replicas:        2,
				IngressClass:    tt.ingressClass,
				IngressProvider: tt.ingressProvider,
			}

			deployer := NewDeployer(client, logr.Discard(), config, nil)
			ingressClass := deployer.ingressClass()

			if ingressClass == nil {
				t.Fatal("expected ingress class but got nil")
			}

			if ingressClass.Name != tt.ingressClass {
				t.Errorf("expected ingress class name %q, got %q", tt.ingressClass, ingressClass.Name)
			}

			if ingressClass.Spec.Controller != tt.expectedController {
				t.Errorf("expected controller %q, got %q", tt.expectedController, ingressClass.Spec.Controller)
			}

			// Verify it's marked as default class
			if ingressClass.Annotations["ingressclass.kubernetes.io/is-default-class"] != "true" {
				t.Error("expected ingress class to be marked as default")
			}
		})
	}
}
