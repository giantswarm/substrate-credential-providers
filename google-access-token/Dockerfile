# Built from the repository root (the generated CI's build context) with this
# file as the Dockerfile. The provider is built inside the image, so the build
# needs nothing but the module's directory. Both stages are pinned by digest.
# The build stage runs on the builder's own platform and cross-compiles for the
# target: a Go build under emulation takes longer than the image job allows.
FROM --platform=$BUILDPLATFORM gsoci.azurecr.io/giantswarm/golang:1.27.2@sha256:5bc7f572bbaa98885a3a1fd9c0aa76b59e3e14e8628bfc316bbfd0c701e4818c AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src
COPY google-access-token/go.mod google-access-token/go.sum ./
RUN go mod download
COPY google-access-token/ ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /google-access-token-credential-provider ./cmd/google-access-token-credential-provider

FROM gsoci.azurecr.io/giantswarm/alpine:3.24.2@sha256:31b6477333eb8257db9e5d7c3a7264fd0467928756f0bbcc27d35bea5d28cdbd

# The token exchange is an HTTPS call to Google's endpoint: the CA bundle comes
# from the build stage, so this stage runs nothing on the target platform.
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /google-access-token-credential-provider /google-access-token-credential-provider

USER 65532:65532

ENTRYPOINT ["/google-access-token-credential-provider"]
