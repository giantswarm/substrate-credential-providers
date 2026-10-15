# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Added

- `google-access-token`: the `google-access-token.k8s.io` credential provider class as a plugin, a Google access token minted from the service account key a Kubernetes Secret entry holds (JWT bearer exchange at the key's `token_uri`, `cloud-platform` scope), cached per key while fifteen minutes remain; mutual TLS pinned to the egress gateway's SPIFFE ID; default-deny atespace-to-namespace grants in the shape of the reference provider's `namespacePolicies`. The chart `google-access-token-credential-provider` deploys it beside Substrate with the two podCertificate volumes, a NetworkPolicy and a CiliumNetworkPolicy opening `oauth2.googleapis.com:443`. It replaces the provider name the giantswarm/substrate line served from its bundled provider until 1.11 (giantswarm/substrate#250, giantswarm/giantswarm#38547): deploy the chart first, route the class through `credentialProvider.additionalProviders` with Substrate 1.12.0 or later, where the bundled name is gone.

[Unreleased]: https://github.com/giantswarm/substrate-credential-providers/tree/main
