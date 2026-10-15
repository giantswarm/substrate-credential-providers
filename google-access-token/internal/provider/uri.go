// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package provider

import (
	"fmt"
	"net/url"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

// ProviderName is the ate-secret:// URI host this provider serves. Its tail
// names a Kubernetes Secret entry holding a Google service account key, as the
// reference k8s.io provider's tail names one holding a credential.
const ProviderName = "google-access-token.k8s.io"

// uriScheme is the only scheme a credential URI may carry.
const uriScheme = "ate-secret"

// LocalLocator is the reserved leading path segment naming the local Kubernetes
// API server the provider runs in, the only cluster served.
const LocalLocator = "default"

// SecretRef is a parsed ate-secret:// URI of this provider:
//
//	ate-secret://google-access-token.k8s.io/default/<namespace>/<secret>/<key>
type SecretRef struct {
	Namespace string
	Name      string
	// Key is the data key within the Secret holding the service account key.
	Key string
}

// ParseURI parses an ate-secret:// URI of this provider. It rejects any other
// scheme or provider name, and any component the grammar does not define.
func ParseURI(raw string) (SecretRef, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return SecretRef{}, fmt.Errorf("parsing credential URI %q: %w", raw, err)
	}
	if u.Scheme != uriScheme {
		return SecretRef{}, fmt.Errorf("malformed credential URI %q: scheme is %q, want %q", raw, u.Scheme, uriScheme)
	}
	if u.Host != ProviderName {
		return SecretRef{}, fmt.Errorf("credential URI %q: provider is %q, this provider serves %q", raw, u.Host, ProviderName)
	}
	// The grammar is scheme/host/path only; a query or fragment means the caller
	// assumed a syntax this provider does not honor, so reject it rather than
	// silently ignore it.
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return SecretRef{}, fmt.Errorf("credential URI %q: user info, query, and fragment components are not allowed", raw)
	}
	if u.EscapedPath() != u.Path {
		return SecretRef{}, fmt.Errorf("credential URI %q: path must not contain percent-encoding", raw)
	}

	segments := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	for i, s := range segments {
		if s == "" {
			return SecretRef{}, fmt.Errorf("credential URI %q: empty path segment %d", raw, i)
		}
	}
	if segments[0] != LocalLocator {
		return SecretRef{}, fmt.Errorf("credential URI %q: path must begin with %q (only local Secrets are supported), got %q", raw, LocalLocator, segments[0])
	}
	tail := segments[1:]
	if len(tail) != 3 {
		return SecretRef{}, fmt.Errorf("credential URI %q: want %s/<namespace>/<secret>/<key>, got %d trailing segments", raw, LocalLocator, len(tail))
	}
	ref := SecretRef{Namespace: tail[0], Name: tail[1], Key: tail[2]}
	if len(validation.IsDNS1123Label(ref.Namespace)) != 0 || len(validation.IsDNS1123Subdomain(ref.Name)) != 0 || len(validation.IsConfigMapKey(ref.Key)) != 0 {
		return SecretRef{}, fmt.Errorf("credential URI contains an invalid namespace, secret name, or key")
	}
	return ref, nil
}
