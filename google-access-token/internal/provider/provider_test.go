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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/agent-substrate/substrate/pkg/proto/credproviderpb"
)

func TestParseURI(t *testing.T) {
	const good = "ate-secret://google-access-token.k8s.io/default/ns1/vertex/credentials.json"
	for _, tc := range []struct {
		name    string
		uri     string
		want    SecretRef
		wantErr bool
	}{
		{name: "local with key", uri: good, want: SecretRef{Namespace: "ns1", Name: "vertex", Key: "credentials.json"}},
		{name: "key required", uri: "ate-secret://google-access-token.k8s.io/default/ns1/vertex", wantErr: true},
		{name: "reference provider name", uri: "ate-secret://k8s.io/default/ns1/vertex/credentials.json", wantErr: true},
		{name: "other k8s.io subdomain", uri: "ate-secret://vault.k8s.io/default/ns1/vertex/credentials.json", wantErr: true},
		{name: "wrong scheme", uri: "https://google-access-token.k8s.io/default/ns1/vertex/credentials.json", wantErr: true},
		{name: "missing locator", uri: "ate-secret://google-access-token.k8s.io/ns1/vertex/credentials.json", wantErr: true},
		{name: "remote form not supported", uri: "ate-secret://google-access-token.k8s.io/cluster/remote-east/ns1/vertex/credentials.json", wantErr: true},
		{name: "too few segments", uri: "ate-secret://google-access-token.k8s.io/default/ns1", wantErr: true},
		{name: "too many segments", uri: good + "/extra", wantErr: true},
		{name: "query not allowed", uri: good + "?cluster=remote", wantErr: true},
		{name: "fragment not allowed", uri: good + "#x", wantErr: true},
		{name: "percent-encoded separator", uri: "ate-secret://google-access-token.k8s.io/default/ns1/vertex/cred%2Fjson", wantErr: true},
		{name: "percent-encoding of any kind", uri: "ate-secret://google-access-token.k8s.io/default/ns1/vertex/cred%2Djson", wantErr: true},
		{name: "space in path", uri: "ate-secret://google-access-token.k8s.io/default/ns1/vertex/cred json", wantErr: true},
		{name: "user info", uri: "ate-secret://user@google-access-token.k8s.io/default/ns1/vertex/credentials.json", wantErr: true},
		{name: "empty query", uri: good + "?", wantErr: true},
		{name: "empty fragment", uri: good + "#", wantErr: true},
		{name: "trailing slash", uri: good + "/", wantErr: true},
		{name: "empty namespace", uri: "ate-secret://google-access-token.k8s.io/default//vertex/credentials.json", wantErr: true},
		{name: "invalid namespace", uri: "ate-secret://google-access-token.k8s.io/default/NS/vertex/credentials.json", wantErr: true},
		{name: "path traversal", uri: "ate-secret://google-access-token.k8s.io/default/ns1/../credentials.json", wantErr: true},
		{name: "unparseable", uri: "://://", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseURI(tc.uri)
			if tc.wantErr {
				require.Error(t, err, "ParseURI(%q) = %+v", tc.uri, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

const (
	teamAActor = "spiffe://substrate-actor.local/actor/team-a/my-actor"
	teamBActor = "spiffe://substrate-actor.local/actor/team-b/my-actor"
	ns1URI     = "ate-secret://google-access-token.k8s.io/default/ns1/vertex/credentials.json"
)

// The fake clientset is read-only in these tests: a fetch that passes
// authorization reads the Secret and fails on its content, which is the
// exchanger's business (google_test.go); a denied one never reads at all.
func TestFetchSecretAuthorization(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vertex", Namespace: "ns1"},
		Data:       map[string][]byte{"credentials.json": []byte("not a key")},
	}
	authz, err := newNamespaceAuthorizer(namespacePolicyFile{
		Policies: []atespaceNamespacePolicy{{Atespace: "team-a", AllowedNamespaces: []string{"ns1"}}},
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		name          string
		actorSpiffeID string
		uri           string
		wantCode      codes.Code
	}{
		{name: "allowed", actorSpiffeID: teamAActor, uri: ns1URI, wantCode: codes.FailedPrecondition},
		{name: "namespace not permitted", actorSpiffeID: teamAActor, uri: "ate-secret://google-access-token.k8s.io/default/ns2/vertex/credentials.json", wantCode: codes.PermissionDenied},
		{name: "unknown atespace", actorSpiffeID: teamBActor, uri: ns1URI, wantCode: codes.PermissionDenied},
		{name: "ateom identity", actorSpiffeID: "spiffe://substrate-actor.local/ateom-for-actor/team-a/my-actor", uri: ns1URI, wantCode: codes.PermissionDenied},
		{name: "garbage identity", actorSpiffeID: "not-a-spiffe-uri", uri: ns1URI, wantCode: codes.PermissionDenied},
		{name: "bad uri", actorSpiffeID: teamAActor, uri: "ate-secret://vault.io/ns1/vertex", wantCode: codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(secret)
			srv := NewServer(client, authz, DefaultExchangeTimeout)
			_, err := srv.FetchSecret(t.Context(), &credproviderpb.FetchSecretRequest{Uri: tc.uri, ActorSpiffeId: tc.actorSpiffeID})
			require.Equal(t, tc.wantCode, status.Code(err), "err = %v", err)
			if tc.wantCode != codes.FailedPrecondition {
				require.Empty(t, client.Actions(), "denied request reached Kubernetes")
			}
		})
	}

	t.Run("nil authorizer denies", func(t *testing.T) {
		srv := NewServer(fake.NewSimpleClientset(secret), nil, DefaultExchangeTimeout)
		_, err := srv.FetchSecret(t.Context(), &credproviderpb.FetchSecretRequest{Uri: ns1URI, ActorSpiffeId: teamAActor})
		require.Equal(t, codes.PermissionDenied, status.Code(err))
	})
}

func TestFetchSecretSecretLookup(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vertex", Namespace: "ns1"},
		Data:       map[string][]byte{"credentials.json": []byte("not a key")},
	}
	for _, tc := range []struct {
		name     string
		uri      string
		wantCode codes.Code
	}{
		{name: "missing key", uri: "ate-secret://google-access-token.k8s.io/default/ns1/vertex/nope", wantCode: codes.NotFound},
		{name: "secret not found looks like a denial", uri: "ate-secret://google-access-token.k8s.io/default/ns1/absent/credentials.json", wantCode: codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := NewServer(fake.NewSimpleClientset(secret), &NamespaceAuthorizer{allowed: map[string]map[string][]grant{"team-a": {"ns1": {{}}}}}, DefaultExchangeTimeout)
			_, err := srv.FetchSecret(t.Context(), &credproviderpb.FetchSecretRequest{Uri: tc.uri, ActorSpiffeId: teamAActor})
			require.Equal(t, tc.wantCode, status.Code(err), "err = %v", err)
		})
	}
}

func TestFetchSecretKubernetesErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code codes.Code
	}{
		{"forbidden", k8serrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "vertex", errors.New("RBAC")), codes.PermissionDenied},
		{"unavailable", errors.New("upstream response body should stay private"), codes.Unavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset()
			client.PrependReactor("get", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) { return true, nil, tc.err })
			srv := NewServer(client, &NamespaceAuthorizer{allowed: map[string]map[string][]grant{"team-a": {"ns1": {{}}}}}, DefaultExchangeTimeout)
			_, err := srv.FetchSecret(t.Context(), &credproviderpb.FetchSecretRequest{Uri: ns1URI, ActorSpiffeId: teamAActor})
			require.Equal(t, tc.code, status.Code(err), "err = %v", err)
			require.NotContains(t, err.Error(), "stay private", "Kubernetes response body exposed")
		})
	}
}

// A grant narrowed by label admits only Secrets carrying those labels; a grant
// without a selector keeps admitting every Secret in the namespace. The
// refusal is indistinguishable from a missing Secret.
func TestFetchSecretSecretNarrowing(t *testing.T) {
	labeled := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "model-key", Namespace: "ns1", Labels: map[string]string{"example.com/credential": "true"}},
		Data:       map[string][]byte{"credentials.json": []byte("not a key")},
	}
	unlabeled := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "ca-pool", Namespace: "ns1"},
		Data:       map[string][]byte{"credentials.json": []byte("not a key")},
	}
	selector := &secretSelector{MatchLabels: map[string]string{"example.com/credential": "true"}}
	for _, tc := range []struct {
		name     string
		policy   atespaceNamespacePolicy
		secret   string
		wantCode codes.Code
	}{
		{name: "no narrowing admits everything", policy: atespaceNamespacePolicy{Atespace: "team-a", AllowedNamespaces: []string{"ns1"}}, secret: "ca-pool", wantCode: codes.FailedPrecondition},
		{name: "label grant admits a labeled secret", policy: atespaceNamespacePolicy{Atespace: "team-a", AllowedNamespaces: []string{"ns1"}, SecretSelector: selector}, secret: "model-key", wantCode: codes.FailedPrecondition},
		{name: "label grant refuses an unlabeled secret", policy: atespaceNamespacePolicy{Atespace: "team-a", AllowedNamespaces: []string{"ns1"}, SecretSelector: selector}, secret: "ca-pool", wantCode: codes.PermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authz, err := newNamespaceAuthorizer(namespacePolicyFile{Policies: []atespaceNamespacePolicy{tc.policy}})
			require.NoError(t, err)
			srv := NewServer(fake.NewSimpleClientset(labeled, unlabeled), authz, DefaultExchangeTimeout)
			_, err = srv.FetchSecret(t.Context(), &credproviderpb.FetchSecretRequest{
				Uri:           "ate-secret://google-access-token.k8s.io/default/ns1/" + tc.secret + "/credentials.json",
				ActorSpiffeId: teamAActor,
			})
			require.Equal(t, tc.wantCode, status.Code(err), "err = %v", err)
			if tc.wantCode == codes.PermissionDenied {
				require.Contains(t, err.Error(), `atespace "team-a" is not permitted to resolve secret ns1/`+tc.secret)
			}
		})
	}
}
