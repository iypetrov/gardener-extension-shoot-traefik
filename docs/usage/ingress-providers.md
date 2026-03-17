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
          ingressClass: traefik
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
          ingressClass: nginx  # Can use 'nginx' to maintain compatibility
```

**Note:** When `ingressProvider: KubernetesIngressNGINX` is set without specifying `ingressClass`, the IngressClass name is automatically set to `"nginx"` for compatibility.

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
          ingressClass: nginx  # Keep using 'nginx' class name
          replicas: 2
```

**Note:** The `ingressClass: nginx` field is optional when using `KubernetesIngressNGINX` - if not specified, it defaults to `"nginx"` automatically.

**Note:** When using `KubernetesIngressNGINX` provider, the IngressClass resource is created with `controller: k8s.io/ingress-nginx` to maintain compatibility with existing Ingress resources that expect the NGINX controller name. Traefik will handle these Ingresses using its NGINX-compatible provider.

### Step 2: Update IngressClass References (Optional)

If you want to use a different ingress class name:

```yaml
spec:
  ingressProvider: KubernetesIngressNGINX
  ingressClass: traefik  # Use new name
```

Then update your Ingress resources:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: my-app
  annotations:
    # NGINX annotations will be translated by Traefik
    nginx.ingress.kubernetes.io/rewrite-target: /
spec:
  ingressClassName: traefik  # Update to match new class name
  rules:
    - host: myapp.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: my-app
                port:
                  number: 80
```

### Step 3: Test and Validate

1. Deploy your application and Ingress resources
2. Verify that traffic is routed correctly
3. Check Traefik logs for any warnings about unsupported annotations
4. Test all critical paths and features

### Step 4: Transition to Native Traefik (Optional)

Once you've validated that everything works with the NGINX-compatible provider, you can optionally transition to the standard provider and use Traefik-native annotations for better integration:

```yaml
spec:
  ingressProvider: KubernetesIngress  # Switch to standard provider
  ingressClass: traefik
```

Update your Ingress annotations to use Traefik-native annotations. See [Traefik Ingress Documentation](https://doc.traefik.io/traefik/reference/routing-configuration/kubernetes/ingress/) for available annotations.

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
| `spec.ingressProvider` | string | `KubernetesIngress` | Ingress provider type: `KubernetesIngress` or `KubernetesIngressNGINX` |
| `spec.ingressClass` | string | `traefik` | Ingress class name that Traefik will handle |
| `spec.image` | string | from imagevector | Traefik container image |
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
          ingressClass: traefik
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
          ingressClass: nginx
          replicas: 2
```

## Further Reading

- [Traefik Kubernetes Ingress Documentation](https://doc.traefik.io/traefik/reference/install-configuration/providers/kubernetes/kubernetes-ingress/)
- [Traefik NGINX Annotations Support](https://doc.traefik.io/traefik/reference/install-configuration/providers/kubernetes/kubernetes-ingress-nginx/)
- [NGINX to Traefik Migration Guide](https://doc.traefik.io/traefik/migrate/nginx-to-traefik/)
- [Kubernetes Ingress Specification](https://kubernetes.io/docs/concepts/services-networking/ingress/)
