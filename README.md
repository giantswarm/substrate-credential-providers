# substrate-credential-providers

Credential providers for [Agent Substrate](https://github.com/agent-substrate/substrate)'s
egress gateway, one plugin per provider class. A provider is a gRPC service
implementing substrate's `CredentialProvider.FetchSecret`
(`pkg/proto/credproviderpb`) under its own `ate-secret://` URI authority, run as
a Deployment of its own behind mutual TLS; the gateway dials it with its pod
identity and the provider pins the gateway's SPIFFE ID. The layout follows
upstream's `internal/plugins` (one Go module, one image and one chart per
provider), so a provider here is what upstream's own plugins are, outside the
substrate tree.

| Provider | Class | Module | Chart |
|---|---|---|---|
| Google access tokens for Vertex AI | `google-access-token.k8s.io` | [`google-access-token/`](google-access-token/) | [`helm/google-access-token-credential-provider/`](helm/google-access-token-credential-provider/) |

Each module imports substrate's `pkg/proto/credproviderpb` and nothing else of
substrate (a test in each `cmd/` holds the rule), pinned to the
giantswarm/substrate release the platform runs. The Substrate chart routes a
class to its provider through `credentialProvider.additionalProviders`; the
agent-platform meta chart carries that wiring.

## Development

`go.work` joins the provider modules, so the root targets cover them all:

```sh
make test          # go test -race ./...
make lint          # golangci-lint
helm unittest helm/google-access-token-credential-provider
```

The image of a provider is built from the repository root with its Dockerfile:

```sh
podman build -f google-access-token/Dockerfile --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 -t google-access-token-credential-provider:dev .
```

## Adding a provider

A new provider is a sibling directory with its own `go.mod` (added to
`go.work`), a `Dockerfile` built from the repository root, a chart under
`helm/<name>-credential-provider` and, in `.circleci/custom.yml`, its image and
chart jobs beside the generated ones.
