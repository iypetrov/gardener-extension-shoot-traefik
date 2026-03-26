# API Reference

## Packages
- [traefik.extensions.gardener.cloud/v1alpha1](#traefikextensionsgardenercloudv1alpha1)


## traefik.extensions.gardener.cloud/v1alpha1

Package v1alpha1 provides the v1alpha1 version of the external API types.



#### IngressProviderType

_Underlying type:_ _string_

IngressProviderType defines the type of Kubernetes Ingress provider to use.



_Appears in:_
- [TraefikConfigSpec](#traefikconfigspec)

| Field | Description |
| --- | --- |
| `KubernetesIngress` | IngressProviderKubernetesIngress is the standard Kubernetes Ingress provider.<br /> |
| `KubernetesIngressNGINX` | IngressProviderKubernetesIngressNGINX is the NGINX-compatible Kubernetes Ingress provider.<br />This provider supports NGINX Ingress Controller annotations, making it easier to migrate<br />from NGINX Ingress Controller to Traefik.<br /> |




#### TraefikConfigSpec



TraefikConfigSpec defines the desired state of [TraefikConfig]



_Appears in:_
- [TraefikConfig](#traefikconfig)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `replicas` _integer_ | Replicas is the number of Traefik replicas to deploy.<br />Defaults to 2 if not specified. |  |  |
| `ingressProvider` _[IngressProviderType](#ingressprovidertype)_ | IngressProvider specifies which Kubernetes Ingress provider to use.<br />Valid values are:<br />- "KubernetesIngress" (default): Standard Kubernetes Ingress provider<br />- "KubernetesIngressNGINX": NGINX-compatible provider with support for NGINX annotations<br />Use KubernetesIngressNGINX when migrating from NGINX Ingress Controller to maintain<br />compatibility with existing NGINX-specific annotations. |  |  |
| `logLevel` _string_ | LogLevel sets the Traefik log level.<br />Valid values are: DEBUG, INFO, WARN, ERROR, FATAL, PANIC<br />Defaults to "INFO" if not specified. |  |  |


