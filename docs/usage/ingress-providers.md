# Traefik Ingress Providers

This document describes the different Kubernetes Ingress provider types supported by the Traefik extension and how to configure them.

## Overview

The Traefik extension supports two Kubernetes Ingress provider types:

1. **KubernetesIngress** (default) - Standard Kubernetes Ingress provider
2. **KubernetesIngressNGINX** - NGINX-compatible provider with annotation support

## Provider Types

### KubernetesIngress (Default)

The standard Kubernetes Ingress provider implements the core [Kubernetes Ingress specification](https://kubernetes.io/docs/concepts/services-networking/ingress/). This is the default provider and is suitable for most use cases.

**Configuration:**

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: my-shoot
  namespace: garden-my-project
spec:
  purpose: evaluation
  extensions:
    - type: traefik
      providerConfig:
        apiVersion: traefik.extensions.gardener.cloud/v1alpha1
        kind: TraefikConfig
        spec:
          ingressProvider: KubernetesIngress
```

**When to use:**
- New deployments without legacy NGINX Ingress Controller dependencies
- Standard Kubernetes Ingress features are sufficient
- You want to use Traefik-native annotations and features

### KubernetesIngressNGINX

The NGINX-compatible provider makes it easier to migrate from NGINX Ingress Controller to Traefik with minimal configuration changes. Note that only a subset of NGINX annotations is supported — see the [Traefik NGINX Annotations Support](https://doc.traefik.io/traefik/reference/routing-configuration/kubernetes/ingress-nginx/) page for details.

**Configuration:**

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: my-shoot
  namespace: garden-my-project
spec:
  purpose: evaluation
  extensions:
    - type: traefik
      providerConfig:
        apiVersion: traefik.extensions.gardener.cloud/v1alpha1
        kind: TraefikConfig
        spec:
          ingressProvider: KubernetesIngressNGINX
```

The ingress class is automatically set to `"nginx"` when using this provider.

**When to use:**
- Migrating from NGINX Ingress Controller to Traefik
- Your existing Ingress resources use NGINX-specific annotations
- You want to maintain compatibility with NGINX annotations during transition
- You need minimal changes to existing Ingress configurations

**Supported NGINX Annotations:**

Not all NGINX annotations are supported. For the complete list of annotations that Traefik can translate, see:
- [Traefik NGINX Annotations Support](https://doc.traefik.io/traefik/reference/routing-configuration/kubernetes/ingress-nginx/)

## Migration from NGINX Ingress Controller

If you're migrating from NGINX Ingress Controller, follow these steps:

### Step 1: Configure the Extension

Enable the Traefik extension with the NGINX-compatible provider:

```yaml
spec:
  purpose: evaluation
  extensions:
    - type: traefik
      providerConfig:
        apiVersion: traefik.extensions.gardener.cloud/v1alpha1
        kind: TraefikConfig
        spec:
          ingressProvider: KubernetesIngressNGINX
          replicas: 2
```

The ingress class is automatically set to `"nginx"` for compatibility with existing Ingress resources.

### Step 2: Verify Ingress Resources

1. Deploy your application and Ingress resources
2. Verify that traffic is routed correctly
3. Check Traefik logs for any warnings about unsupported annotations
4. Test all critical paths and features

### Step 3: Transition to Native Traefik (Optional)

Once you've validated that everything works with the NGINX-compatible provider, you can optionally transition to the standard provider and use Traefik-native annotations for better integration:

```yaml
spec:
  ingressProvider: KubernetesIngress  # Switch to standard provider
```

The ingress class will automatically change to `"traefik"`. Update your Ingress resources accordingly and switch to Traefik-native annotations. See [Traefik Ingress Documentation](https://doc.traefik.io/traefik/reference/routing-configuration/kubernetes/ingress/) for available annotations.

## RBAC Permissions

The extension automatically configures the appropriate RBAC permissions based on the selected provider:

### KubernetesIngress Provider
- Standard Kubernetes resources (Services, Endpoints, Secrets, Nodes)
- Ingress resources and IngressClasses
- EndpointSlices
- Traefik CRDs

### KubernetesIngressNGINX Provider
Includes all of the above plus:
- Namespace resources (required for namespace selectors)

## Configuration Reference

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `spec.ingressProvider` | string | `KubernetesIngress` | Ingress provider type: `KubernetesIngress` (ingress class: `traefik`) or `KubernetesIngressNGINX` (ingress class: `nginx`) |
| `spec.replicas` | int32 | `2` | Number of Traefik replicas |

## Examples

### Standard Kubernetes Ingress

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: standard-ingress-shoot
spec:
  purpose: evaluation
  extensions:
    - type: traefik
      providerConfig:
        apiVersion: traefik.extensions.gardener.cloud/v1alpha1
        kind: TraefikConfig
        spec:
          ingressProvider: KubernetesIngress
          replicas: 3
```

### NGINX-Compatible Ingress

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: nginx-compat-shoot
spec:
  purpose: evaluation
  extensions:
    - type: traefik
      providerConfig:
        apiVersion: traefik.extensions.gardener.cloud/v1alpha1
        kind: TraefikConfig
        spec:
          ingressProvider: KubernetesIngressNGINX
          replicas: 2
```

## Further Reading

- [Traefik Kubernetes Ingress Documentation](https://doc.traefik.io/traefik/reference/install-configuration/providers/kubernetes/kubernetes-ingress/)
- [Traefik NGINX Annotations Support](https://doc.traefik.io/traefik/reference/install-configuration/providers/kubernetes/kubernetes-ingress-nginx/)
- [NGINX to Traefik Migration Guide](https://doc.traefik.io/traefik/migrate/nginx-to-traefik/)
- [Kubernetes Ingress Specification](https://kubernetes.io/docs/concepts/services-networking/ingress/)
