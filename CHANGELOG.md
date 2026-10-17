# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

## [0.1.0-rc.5] - 2026-10-15

### Fixed

- `google-access-token` image: the build stage cross-compiles from the builder's platform and the final stage runs nothing, so the arm64 image builds within the image job's time instead of compiling under emulation.

## [0.1.0-rc.2] - 2026-10-15

### Added

- `google-access-token`: a credential provider for the `google-access-token.k8s.io` class. It resolves an `ate-secret://google-access-token.k8s.io/default/<namespace>/<secret>/<key>` URI naming a Google service account key to an access token with the `cloud-platform` scope. Only the egress gateway may call it, and each atespace resolves keys only in the namespaces its `namespacePolicies` grant.
- `google-access-token-credential-provider` chart: deploys the provider into Substrate's namespace with a NetworkPolicy and a CiliumNetworkPolicy that allows egress to Google's token endpoint. Route the class to it through the Substrate chart's `credentialProvider.additionalProviders`; Substrate 1.12.0 and later no longer serve the class from the bundled provider.

[Unreleased]: https://github.com/giantswarm/substrate-credential-providers/compare/v0.1.0-rc.5...HEAD
[0.1.0-rc.5]: https://github.com/giantswarm/substrate-credential-providers/compare/v0.1.0-rc.2...v0.1.0-rc.5
[0.1.0-rc.2]: https://github.com/giantswarm/substrate-credential-providers/releases/tag/v0.1.0-rc.2
