# Built from the repository root (the generated CI's build context) with this
# file as the Dockerfile. CircleCI's architect/go-build compiles the provider
# from the git checkout, so the binary records its commit, and attaches it to
# the build context as substrate-credential-providers-<os>-<arch>; this image
# only assembles the runtime. For a local build, produce the binary first:
#   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o substrate-credential-providers-linux-amd64 ./google-access-token/cmd/google-access-token-credential-provider
FROM gsoci.azurecr.io/giantswarm/alpine:3.24.2@sha256:31b6477333eb8257db9e5d7c3a7264fd0467928756f0bbcc27d35bea5d28cdbd

ARG TARGETOS
ARG TARGETARCH
COPY substrate-credential-providers-${TARGETOS}-${TARGETARCH} /google-access-token-credential-provider

USER 65532:65532

ENTRYPOINT ["/google-access-token-credential-provider"]
