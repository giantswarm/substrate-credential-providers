# google-access-token credential provider

An [Agent Substrate](https://github.com/agent-substrate/substrate) credential
provider for Google APIs. It implements substrate's `CredentialProvider` gRPC
API (`pkg/proto/credproviderpb`) for egress credential injection: given an
`ate-secret://google-access-token.k8s.io/...` URI naming a Kubernetes Secret
entry that holds a Google service account key, it signs the key's JWT
assertion, exchanges it at the key's `token_uri` for an access token scoped to
`https://www.googleapis.com/auth/cloud-platform`, and returns the token. The
key stays in the cluster; the egress gateway and the actor only ever see a
token that expires within the hour. Vertex AI is the first consumer: a kagent
`ModelConfig` of the `GeminiVertexAI` or `AnthropicVertexAI` provider with a
service account key Secret binds this URI on the Vertex AI hostname.

This directory is a Go module of its own. It imports substrate's
`pkg/proto/credproviderpb` and nothing else of substrate; the module is pinned
to the giantswarm/substrate release the platform runs.

## Endpoint

The chart in `helm/google-access-token-credential-provider` deploys the
provider into substrate's `ate-system` namespace:

| | |
|---|---|
| Provider name | `google-access-token.k8s.io`, the host of its `ate-secret://` credential URIs |
| Address | `google-access-token-credential-provider.ate-system.svc:50051` |
| API | `credprovider.CredentialProvider/FetchSecret` over mutual TLS |

The provider's serving certificate names the Service's DNS name, so the
gateway must use exactly this address. The Substrate chart routes the class to
it through `credentialProvider.additionalProviders`.

## Credential URIs

```
ate-secret://google-access-token.k8s.io/default/<namespace>/<secret>/<key>
```

- `default` is the locator of the local cluster, the only one served.
- `<namespace>` and `<secret>` name the Kubernetes Secret.
- `<key>` is the Secret entry that holds the service account key in the JSON
  form Google issues. It is required.

User information, queries, fragments, percent-encoding and empty path segments
are refused, as is any other provider name.

## Responses

`FetchSecret` returns the access token in `opaque_bytes`. On each fetch the
provider reads the Secret, signs the key's JWT assertion and exchanges it at
the key's `token_uri` (`https://oauth2.googleapis.com/token` for keys Google
issues) for a token scoped to `https://www.googleapis.com/auth/cloud-platform`.
The service account's IAM roles decide what the token may do. The `token_uri`
has to be an HTTPS URL on a host the chart's `networkPolicy.cilium.tokenHosts`
lists.

Tokens live one hour. The provider caches each token per key and hands it out
while at least fifteen minutes remain, which keeps the egress gateway's
five-minute cache from serving an expired token and exchanges a key about once
every 45 minutes. Concurrent fetches of one key share a single exchange. The
cache is keyed on the Secret's contents, so a rotated key is exchanged on its
next fetch.

| Condition | Code |
|---|---|
| The URI is malformed, lacks the key, or names another provider | `InvalidArgument` |
| The actor's atespace is not granted the URI's namespace, the actor identity is unusable, the Secret does not exist or does not match the grant's label selector, or the provider may not read it | `PermissionDenied` |
| The Secret has no such key | `NotFound` |
| The entry is not a Google service account key, its `token_uri` is not HTTPS, or Google rejects the key | `FailedPrecondition` |
| The token endpoint cannot be reached, answers without a usable token, or issues one too short-lived to outlive the gateway cache; or Kubernetes cannot be read | `Unavailable` |

A missing Secret and one the grant's label selector refuses give the same
answer, so a caller cannot probe which Secrets exist. Error messages name the
Secret and the service account, never the key or the token, and the private
key and the token are never logged.

## Security model

- **Only the egress gateway may call.** The provider serves mutual TLS with a
  certificate from substrate's servicedns signer, and admits a caller only if
  its certificate chains to substrate's podidentity trust bundle and carries
  the egress gateway's SPIFFE ID (`--injector-identity`).
- **Each atespace resolves keys only in the namespaces its policy grants.**
  The provider takes the atespace from the actor SPIFFE ID the gateway sends
  and refuses, before reading anything, a URI whose namespace the
  `--namespace-policy-file` policy does not list for it. The policy is
  default-deny and has the shape of the reference Kubernetes provider's
  `namespacePolicies`; a `secretSelector` narrows a grant to Secrets carrying
  every listed label. A policy may select namespaces by label instead of
  listing them: an `ownNamespaceSelector` lets each matched namespace be read
  by the atespace of the same name only (never by `ate-golden`), and an
  `allowedNamespaceSelector` beside `atespace` grants that atespace every
  matched namespace. The provider then watches namespaces and matches their
  labels at each request, so a namespace labeled or unlabeled after start is
  granted or refused without a restart.
- **The provider's own Kubernetes identity bounds the rest.** It holds `get`
  on Secrets cluster-wide, and `list` and `watch` on namespaces only when a
  policy selects them; the grants decide which Secrets an actor reaches.
- **The exchange leaves the cluster.** The pod reaches Google's token endpoint
  on 443 and nothing else; the chart's CiliumNetworkPolicy says so.

## Flags

| Flag | Default | |
|---|---|---|
| `--listen-address` | `:50051` | gRPC |
| `--health-address` | `:9090` | `/healthz` always 200; `/readyz` 503 while draining |
| `--server-cred-bundle` | required | the serving credential bundle (PKCS#8 key and chain), reloaded on rotation |
| `--client-ca-file` | required | the trust bundle callers chain to, reloaded on rotation |
| `--injector-identity` | required | the SPIFFE ID a caller must carry |
| `--namespace-policy-file` | required | the grants YAML, read once at start; namespace selectors follow labels without a restart |
| `--exchange-timeout` | `15s` | one round trip to the token endpoint |
| `--drain-grace` | `5s` | in-flight RPCs on shutdown before a hard stop |
| `--kube-api-qps`, `--kube-api-burst` | `50`, `100` | the Secret reads and the namespace watch |

## Development

```sh
go test -race ./...
golangci-lint run
govulncheck ./...
# from the repository root: the image copies the binary CI's go-build attaches
(cd .. && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o substrate-credential-providers-linux-amd64 ./google-access-token/cmd/google-access-token-credential-provider \
  && podman build -f google-access-token/Dockerfile --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 -t google-access-token-credential-provider:dev .)
```

The policy file:

```yaml
policies:
  - atespace: kagent
    allowedNamespaces: [kagent]
  - ownNamespaceSelector:
      matchExpressions:
        - {key: giantswarm.io/organization, operator: Exists}
  - atespace: ate-golden
    allowedNamespaceSelector:
      matchExpressions:
        - {key: giantswarm.io/organization, operator: Exists}
```
