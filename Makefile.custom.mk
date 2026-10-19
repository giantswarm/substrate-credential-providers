##@ Providers

PROVIDERS := google-access-token

# go.work joins the provider modules, so `go list -m` names them all; the
# generated targets want the root module alone.
override APPLICATION := substrate-credential-providers
override MODULE := github.com/giantswarm/substrate-credential-providers

# The generated test target runs in the root module, which holds no code; the
# providers' tests run as its prerequisite, with the race detector where the
# generated target has one (a C toolchain).
test: test-providers

.PHONY: test-providers
test-providers: ## go test in every provider module.
	@for p in $(PROVIDERS); do (cd $$p && go test $(RACE) ./...) || exit 1; done

.PHONY: lint-providers
lint-providers: ## golangci-lint and govulncheck in every provider module.
	@for p in $(PROVIDERS); do (cd $$p && golangci-lint run ./... && go tool govulncheck ./...) || exit 1; done

.PHONY: build-providers
build-providers: ## Build every provider's image from the repository root, from the binary CI's go-build would attach.
	@for p in $(PROVIDERS); do CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o $(APPLICATION)-linux-amd64 ./$$p/cmd/$$p-credential-provider && docker build -f $$p/Dockerfile --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 -t $$p-credential-provider:dev . || exit 1; done

.PHONY: test-charts
test-charts: ## helm unittest over every chart.
	@for c in helm/*/; do helm unittest $$c || exit 1; done
