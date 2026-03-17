# gardener-extension-shoot-traefik

The `gardener-extension-shoot-traefik` deploys Traefik ingress controller to Gardener shoot clusters as a replacement for the nginx-ingress-controller which is out of maintenance.

## Features

- **Traefik Ingress Controller**: Deploys Traefik v3.x as the ingress controller in shoot clusters
- **Admission Webhook**: Validates that Traefik extension is only enabled for shoots with purpose "evaluation"
- **ManagedResource**: Uses Gardener's ManagedResource mechanism for deployment and lifecycle management
- **Configurable**: Supports custom Traefik image, replicas, and ingress class configuration

## Requirements

- [Go 1.26.x](https://go.dev/) or later
- [GNU Make](https://www.gnu.org/software/make/)
- [Docker](https://www.docker.com/) for local development
- [Gardener Local Setup](https://gardener.cloud/docs/gardener/local_setup/) for local development
- Shoot clusters with purpose "evaluation"

## Usage

You can enable the extension for a [Gardener Shoot
cluster](https://gardener.cloud/docs/glossary/_index#gardener-glossary) by
updating the `.spec.extensions` of your shoot manifest.

**Important**: The Traefik extension can only be enabled for shoots with `purpose: evaluation`.
This is enforced by an admission webhook.

```yaml
apiVersion: core.gardener.cloud/v1beta1
kind: Shoot
metadata:
  name: my-shoot
  namespace: garden-my-project
spec:
  # Purpose MUST be "evaluation" for Traefik extension
  purpose: evaluation
  extensions:
    - type: traefik
      providerConfig:
        apiVersion: traefik.extensions.gardener.cloud/v1alpha1
        kind: TraefikConfig
        spec:
          # Optional: Number of replicas (default: 2)
          replicas: 2
          # Optional: Ingress class name (default: traefik)
          ingressClass: traefik
          # Optional: Log level (default: INFO)
          # Valid values: DEBUG, INFO, WARN, ERROR, FATAL, PANIC
          logLevel: INFO
          # Optional: Ingress provider type (default: KubernetesIngress)
          # Valid values:
          # - KubernetesIngress: Standard Kubernetes Ingress provider
          # - KubernetesIngressNGINX: NGINX-compatible provider with NGINX annotation support
          ingressProvider: KubernetesIngress
  # ... rest of your shoot configuration
```

### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `spec.image` | string | `traefik:v3.6.10` | Traefik container image |
| `spec.replicas` | int32 | `2` | Number of Traefik replicas |
| `spec.ingressClass` | string | `traefik` | Ingress class name that Traefik handles |
| `spec.logLevel` | string | `INFO` | Traefik log level: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`, `PANIC` |
| `spec.ingressProvider` | string | `KubernetesIngress` | Kubernetes Ingress provider type: `KubernetesIngress` or `KubernetesIngressNGINX` |

### Ingress Provider Types

The extension supports two Kubernetes Ingress provider types:

#### KubernetesIngress (Default)

The standard Kubernetes Ingress provider that implements the core [Kubernetes Ingress specification](https://kubernetes.io/docs/concepts/services-networking/ingress/).

```yaml
spec:
  ingressProvider: KubernetesIngress
```

#### KubernetesIngressNGINX

The NGINX-compatible provider makes it easier to migrate from NGINX Ingress Controller to Traefik with minimal configuration changes. Note that only a subset of NGINX annotations is supported — see the [Traefik NGINX Annotations Support](https://doc.traefik.io/traefik/reference/install-configuration/providers/kubernetes/kubernetes-ingress-nginx/) page for details.

```yaml
spec:
  ingressProvider: KubernetesIngressNGINX
```

**Note:** When using `KubernetesIngressNGINX`, the `ingressClass` defaults to `"nginx"` if not specified, and the IngressClass resource uses `controller: k8s.io/ingress-nginx` for compatibility with existing Ingress resources. Traefik handles these Ingresses using its NGINX-compatible provider.

**When to use KubernetesIngressNGINX:**
- You're migrating from NGINX Ingress Controller
- Your existing Ingress resources use NGINX-specific annotations
- You want to maintain compatibility with NGINX annotations during the transition

For more information, see:
- [Traefik Kubernetes Ingress Documentation](https://doc.traefik.io/traefik/reference/install-configuration/providers/kubernetes/kubernetes-ingress/)
- [Traefik NGINX Annotations Support](https://doc.traefik.io/traefik/reference/install-configuration/providers/kubernetes/kubernetes-ingress-nginx/)
- [NGINX to Traefik Migration Guide](https://doc.traefik.io/traefik/migrate/nginx-to-traefik/)

## Development

In order to build a binary of the extension, you can use the following command.

``` shell
make build
```

The resulting binary can be found in `bin/extension-traefik`.

In order to build a Docker image of the extension, you can use the following
command.

``` shell
make docker-build
```

For local development of the `gardener-extension-shoot-traefik` it is recommended that
you setup a [development Gardener environment](https://gardener.cloud/docs/gardener/local_setup/).

Please refer to the next sections for more information about deploying and
testing the extension in a Gardener development environment.

## Development Environment without Gardener Operator

The following documents describe how to create a Gardener development
environment locally. Please make sure to read them in order to familiarize
yourself with the setup, and also to install any prerequisites.

- [Gardener: Local setup requirements](https://gardener.cloud/docs/gardener/local_setup/)
- [Gardener: Getting Started Locally](https://gardener.cloud/docs/gardener/deployment/getting_started_locally/)

The steps from this section describe how to deploy and develop the extension
against a local development environment, without the
[Gardener Operator](https://gardener.cloud/docs/gardener/concepts/operator/).

In summary, these are the steps you need to follow in order to start a local
development Gardener environment, however, please make sure that you read the
documents above for additional details.

``` shell
make kind-up gardener-up
```

Before you continue with the next steps, make sure that you configure your
`KUBECONFIG` to point to the kubeconfig file created by Gardener for you.

This file will be located in the
`/path/to/gardener/example/gardener-local/kind/local/kubeconfig` path after
creating the dev environment.

``` shell
export KUBECONFIG=/path/to/gardener/example/gardener-local/kind/local/kubeconfig
```

You can use the following command in order to load the OCI image to the nodes of
your local Gardener cluster, which is running in
[kind](https://kind.sigs.k8s.io/).

``` shell
make kind-load-image
```

The Helm charts, which are used by the
[gardenlet](https://gardener.cloud/docs/gardener/concepts/gardenlet/) for
deploying the extension can be pushed to the local OCI registry using the
following command.

``` shell
make helm-load-chart
```

In the [examples/dev-setup](./examples/dev-setup) directory you can find
[kustomize](https://kustomize.io/]) resources, which can be used to create the
`ControllerDeployment` and `ControllerRegistration` resources.

For more information about `ControllerDeployment` and `ControllerRegistration`
resources, please make sure to check the
[Registering Extension Controllers](https://gardener.cloud/docs/gardener/extensions/registration/)
documentation.

The `deploy` target takes care of deploying your extension in a local Gardener
environment. It does the following.

1. Builds a Docker image of the extension
2. Loads the image into the `kind` cluster nodes
3. Packages the Helm charts and pushes them to the local registry
4. Deploys the `ControllerDeployment` and `ControllerRegistration` resources

``` shell
make deploy
```

Verify that we have successfully created the `ControllerDeployment` and
`ControllerRegistration` resources.

``` shell
$ kubectl get controllerregistrations,controllerdeployments gardener-extension-shoot-traefik
NAME                                                                          RESOURCES           AGE
controllerregistration.core.gardener.cloud/gardener-extension-shoot-traefik   Extension/traefik   40s

NAME                                                                        AGE
controllerdeployment.core.gardener.cloud/gardener-extension-shoot-traefik   40s
```

Finally, we can create an example shoot with our extension enabled. The
[examples/shoot.yaml](./examples/shoot.yaml) file provides a ready-to-use shoot
manifest with the extension enabled and configured.

``` shell
kubectl apply -f examples/shoot.yaml
```

Once we create the shoot cluster, `gardenlet` will start deploying our
`gardener-extension-shoot-traefik`, since it is required by our shoot.

Verify that the extension has been successfully installed by checking the
corresponding `ControllerInstallation` resource.

``` shell
$ kubectl get controllerinstallations.core.gardener.cloud
NAME                                     REGISTRATION                       SEED    VALID   INSTALLED   HEALTHY   PROGRESSING   AGE
gardener-extension-shoot-traefik-tktwt   gardener-extension-shoot-traefik   local   True    True        True      False         103s
```

After your shoot cluster has been successfully created and reconciled, verify
that the extension is healthy.

``` shell
$ kubectl --namespace shoot--local--local get extensions
NAME      TYPE      STATUS      AGE
traefik   traefik   Succeeded   85m
```

In order to trigger reconciliation of the extension you can annotate the
extension resource.

``` shell
kubectl --namespace shoot--local--local annotate extensions traefik gardener.cloud/operation=reconcile
```

## Development Environment with Gardener Operator

The extension can also be deployed via the
[Gardener Operator](https://gardener.cloud/docs/gardener/concepts/operator/).

In order to start a local development environment with the Gardener Operator,
please refer to the following documentations.

- [Gardener Operator](https://gardener.cloud/docs/gardener/concepts/operator/)
- [Gardener: Local setup with gardener-operator](https://gardener.cloud/docs/gardener/deployment/getting_started_locally/#alternative-way-to-set-up-garden-and-seed-leveraging-gardener-operator)

In summary, these are the steps you need to follow in order to start a local
development environment with the [Gardener Operator](https://gardener.cloud/docs/gardener/concepts/operator/),
however, please make sure that you read the documents above for additional details.

``` shell
make kind-multi-zone-up operator-up operator-seed-up
```

Before you continue with the next steps, make sure that you configure your
`KUBECONFIG` to point to the kubeconfig file of the cluster, which runs the
Gardener Operator.

There will be two kubeconfig files created for you, after the dev environment
has been created.

| Path                                                                  | Description                                                         |
|-----------------------------------------------------------------------|---------------------------------------------------------------------|
| `/path/to/gardener/example/gardener-local/kind/multi-zone/kubeconfig` | Cluster in which `gardener-operator` runs (a.k.a _runtime_ cluster) |
| `/path/to/gardener/dev-setup/kubeconfigs/virtual-garden/kubeconfig`   | The _virtual_ garden cluster                                        |

Throughout this document we will refer to the kubeconfigs for _runtime_ and
_virtual_ clusters as `$KUBECONFIG_RUNTIME` and `$KUBECONFIG_VIRTUAL`
respectively.

Before deploying the extension we need to target the _runtime_ cluster, since
this is where the extension resources for `gardener-operator` reside.

``` shell
export KUBECONFIG=$KUBECONFIG_RUNTIME
```

In order to deploy the extension, execute the following command.

``` shell
make deploy-operator
```

The `deploy-operator` target takes care of the following.

1. Builds a Docker image of the extension
2. Loads the image into the `kind` cluster nodes
3. Packages the Helm charts and pushes them to the local registry
4. Deploys the `Extension` (from group `operator.gardener.cloud/v1alpha1`) to
   the _runtime_ cluster

Verify that we have successfully created the
`Extension` (from group `operator.gardener.cloud/v1alpha1`) resource.

``` shell
$ kubectl --kubeconfig $KUBECONFIG_RUNTIME get extop gardener-extension-shoot-traefik
NAME                               INSTALLED   REQUIRED RUNTIME   REQUIRED VIRTUAL   AGE
gardener-extension-shoot-traefik   True        False              False              85s
```

Verify that the respective `ControllerRegistration` and `ControllerDeployment`
resources have been created by the `gardener-operator` in the _virtual_ garden
cluster.

``` shell
> kubectl --kubeconfig $KUBECONFIG_VIRTUAL get controllerregistrations,controllerdeployments gardener-extension-shoot-traefik
NAME                                                                          RESOURCES           AGE
controllerregistration.core.gardener.cloud/gardener-extension-shoot-traefik   Extension/traefik   3m50s

NAME                                                                        AGE
controllerdeployment.core.gardener.cloud/gardener-extension-shoot-traefik   3m50s
```

Now we can create an example shoot with our extension enabled. The
[examples/shoot.yaml](./examples/shoot.yaml) file provides a ready-to-use shoot
manifest, which we will use.

``` shell
kubectl --kubeconfig $KUBECONFIG_VIRTUAL apply -f examples/shoot.yaml
```

Once we create the shoot cluster, `gardenlet` will start deploying our
`gardener-extension-shoot-traefik`, since it is required by our shoot.

Verify that the extension has been successfully installed by checking the
corresponding `ControllerInstallation` resource for our extension.

``` shell
$ kubectl --kubeconfig $KUBECONFIG_VIRTUAL get controllerinstallations.core.gardener.cloud
NAME                                     REGISTRATION                       SEED    VALID   INSTALLED   HEALTHY   PROGRESSING   AGE
gardener-extension-shoot-traefik-ng4r8   gardener-extension-shoot-traefik   local   True    True        True      False         2m9s
```

After your shoot cluster has been successfully created and reconciled, verify
that the extension is healthy.

``` shell
$ kubectl --kubeconfig $KUBECONFIG_RUNTIME --namespace shoot--local--local get extensions
NAME      TYPE      STATUS      AGE
traefik   traefik   Succeeded   2m37s
```

In order to trigger reconciliation of the extension you can annotate the
extension resource.

``` shell
kubectl --kubeconfig $KUBECONFIG_RUNTIME --namespace shoot--local--local annotate extensions traefik gardener.cloud/operation=reconcile
```

# Contributing

`gardener-extension-shoot-traefik` is hosted on
[Github](https://github.com/gardener/gardener-extension-shoot-traefik).

Please contribute by reporting issues, suggesting features or by sending patches
using pull requests.

# License

This project is Open Source and licensed under [Apache License 2.0](https://www.apache.org/licenses/LICENSE-2.0).
