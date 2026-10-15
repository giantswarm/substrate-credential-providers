# google-access-token-credential-provider

Agent Substrate credential provider for the google-access-token.k8s.io class, minting Google access tokens for Vertex AI from service account keys held in Kubernetes Secrets

**Homepage:** <https://github.com/giantswarm/substrate-credential-providers>

An [Agent Substrate](https://github.com/agent-substrate/substrate) credential
provider plugin: a gRPC service the egress gateway calls to resolve
`ate-secret://google-access-token.k8s.io/default/<namespace>/<secret>/<key>`
to an OAuth 2.0 access token minted from the Google service account key the
Secret entry holds. The key never leaves the cluster. See the
[provider's README](../../google-access-token/README.md) for the URI grammar,
the responses and the security model.

## Wiring

The chart installs beside Substrate, in its namespace, and the Substrate chart
routes the class to it:

```yaml
credentialProvider:
  additionalProviders:
    - uriAuthority: google-access-token.k8s.io
      host: google-access-token-credential-provider.ate-system.svc:50051
```

A Substrate release that still serves `google-access-token.k8s.io` from its
bundled provider (the giantswarm/substrate line before 1.12.0) refuses that
entry; deploy the chart first and add the routing with the Substrate release
that dropped the bundled name.

`namespacePolicies` takes the grants of the Substrate chart's
`credentialProvider.namespacePolicies`: an actor's atespace may resolve keys
only from the namespaces listed for it.

The provider exchanges keys at `oauth2.googleapis.com`; the chart's
CiliumNetworkPolicy opens that name on 443 beside DNS and the kube-apiserver.
A cluster whose DNS pods carry other labels than the ones under
`networkPolicy.cilium.dns` must set them.

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| Giant Swarm |  | <https://www.giantswarm.io> |

## Source Code

* <https://github.com/giantswarm/substrate-credential-providers>

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| enabled | bool | `true` |  |
| name | string | `"google-access-token-credential-provider"` |  |
| serviceType | string | `"managed"` |  |
| fullnameOverride | string | `""` |  |
| image.registry | string | `"gsoci.azurecr.io"` |  |
| image.name | string | `"giantswarm/substrate-credential-providers/google-access-token"` |  |
| image.tag | string | `""` |  |
| image.digest | string | `""` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| replicaCount | int | `1` |  |
| logLevel | string | `"info"` |  |
| substrate.releaseName | string | `"substrate"` |  |
| substrate.namespace | string | `"ate-system"` |  |
| substrate.trustDomain | string | `"cluster.local"` |  |
| substrate.egressGateway | string | `"atenet-egress"` |  |
| namespacePolicies | list | `[]` |  |
| exchangeTimeout | string | `"15s"` |  |
| podCertificates.maxExpirationSeconds | string | `nil` |  |
| proxy.httpsProxy | string | `""` |  |
| proxy.noProxy | string | `""` |  |
| networkPolicy.enabled | bool | `true` |  |
| networkPolicy.cilium.enabled | bool | `true` |  |
| networkPolicy.cilium.tokenHosts[0] | string | `"oauth2.googleapis.com"` |  |
| networkPolicy.cilium.dns[0].namespace | string | `"kube-system"` |  |
| networkPolicy.cilium.dns[0].labels.k8s-app | string | `"kube-dns"` |  |
| networkPolicy.cilium.dns[1].namespace | string | `"kube-system"` |  |
| networkPolicy.cilium.dns[1].labels.k8s-app | string | `"coredns"` |  |
| networkPolicy.cilium.dns[2].namespace | string | `"kube-system"` |  |
| networkPolicy.cilium.dns[2].labels.k8s-app | string | `"k8s-dns-node-cache"` |  |
| resources.requests.cpu | string | `"10m"` |  |
| resources.requests.memory | string | `"64Mi"` |  |
| resources.limits.cpu | string | `"500m"` |  |
| resources.limits.memory | string | `"256Mi"` |  |
| serviceAccount.annotations | object | `{}` |  |
| podAnnotations | object | `{}` |  |
| podLabels | object | `{}` |  |
| terminationGracePeriodSeconds | int | `15` |  |
| nodeSelector | object | `{}` |  |
| tolerations | list | `[]` |  |
| affinity | object | `{}` |  |
| podSecurityContext.runAsNonRoot | bool | `true` |  |
| podSecurityContext.runAsUser | int | `65532` |  |
| podSecurityContext.runAsGroup | int | `65532` |  |
| podSecurityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
| securityContext.allowPrivilegeEscalation | bool | `false` |  |
| securityContext.privileged | bool | `false` |  |
| securityContext.readOnlyRootFilesystem | bool | `true` |  |
| securityContext.runAsNonRoot | bool | `true` |  |
| securityContext.runAsUser | int | `65532` |  |
| securityContext.capabilities.drop[0] | string | `"ALL"` |  |
| securityContext.seccompProfile.type | string | `"RuntimeDefault"` |  |
