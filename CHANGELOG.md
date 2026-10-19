# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).



## [Unreleased]

### Added

- `google-access-token`: a namespace policy can select namespaces by label, as Substrate's Kubernetes credential provider does since 1.12.0. `ownNamespaceSelector`, in place of `atespace` and `allowedNamespaces`, lets each matched namespace be read by the atespace of the same name only (never `ate-golden`); `allowedNamespaceSelector` beside `atespace` grants that atespace every matched namespace. The provider watches namespaces only when a policy selects them, and follows label changes without a restart.
- `google-access-token-credential-provider` chart: a ClusterRole with `list` and `watch` on namespaces when a policy has either selector.

- `google-access-token`: a credential provider for the `google-access-token.k8s.io` class. It resolves an `ate-secret://google-access-token.k8s.io/default/<namespace>/<secret>/<key>` URI naming a Google service account key to an access token with the `cloud-platform` scope. Only the egress gateway may call it, and each atespace resolves keys only in the namespaces its `namespacePolicies` grant.
- `google-access-token-credential-provider` chart: deploys the provider into Substrate's namespace with a NetworkPolicy and a CiliumNetworkPolicy that allows egress to Google's token endpoint. Route the class to it through the Substrate chart's `credentialProvider.additionalProviders`; Substrate 1.12.0 and later no longer serve the class from the bundled provider.

### Fixed

- `google-access-token` image: the build stage cross-compiles from the builder's platform and the final stage runs nothing, so the arm64 image builds within the image job's time instead of compiling under emulation.

[Unreleased]: https://github.com/giantswarm/substrate-credential-providers/tree/main
