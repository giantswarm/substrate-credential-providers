##@ Providers

PROVIDERS := google-access-token

# The generated test target runs in the root module, which holds no code; the
# providers' tests run as its prerequisite.
test: test-providers

.PHONY: test-providers
test-providers: ## go test -race in every provider module.
	@for p in $(PROVIDERS); do (cd $$p && go test -race ./...) || exit 1; done

.PHONY: lint-providers
lint-providers: ## golangci-lint and govulncheck in every provider module.
	@for p in $(PROVIDERS); do (cd $$p && golangci-lint run ./... && go run golang.org/x/vuln/cmd/govulncheck@latest ./...) || exit 1; done

.PHONY: build-providers
build-providers: ## Build every provider's image from the repository root.
	@for p in $(PROVIDERS); do docker build -f $$p/Dockerfile --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 -t $$p-credential-provider:dev . || exit 1; done

.PHONY: test-charts
test-charts: ## helm unittest over every chart.
	@for c in helm/*/; do helm unittest $$c || exit 1; done
