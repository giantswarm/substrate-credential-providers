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
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/agent-substrate/substrate/pkg/proto/credproviderpb"
)

const tenantLabel = "example.com/tenant"

// granted is the code of a request that passed authorization: every Secret
// here holds a value that is not a service account key, so the exchange
// refuses it after the grant admitted it.
const granted = codes.FailedPrecondition

// tenantPolicy is the policy file of a platform whose tenants each run their
// actors in the atespace named after their namespace, next to a static grant.
const tenantPolicy = `policies:
- atespace: platform
  allowedNamespaces: [platform]
- ownNamespaceSelector:
    matchExpressions:
    - key: example.com/tenant
      operator: Exists
`

// goldenPolicy opts the golden atespace in to every tenant namespace, beside
// the namespace it lists.
const goldenPolicy = `policies:
- atespace: ate-golden
  allowedNamespaces: [platform]
  allowedNamespaceSelector:
    matchExpressions:
    - key: example.com/tenant
      operator: Exists
`

func namespace(name string, labels map[string]string) *corev1.Namespace {
	return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
}

func keySecret(ns string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vertex", Namespace: ns},
		Data:       map[string][]byte{"credentials.json": []byte("not a key")},
	}
}

// watchedClient returns a fake clientset whose namespace watch is known to be
// open once the returned channel closes: the fake drops what changes between
// an informer's list and its watch.
func watchedClient(objects ...runtime.Object) (*fake.Clientset, <-chan struct{}) {
	client := fake.NewClientset(objects...)
	started := make(chan struct{})
	var once sync.Once
	client.PrependWatchReactor("namespaces", func(action k8stesting.Action) (bool, watch.Interface, error) {
		w, err := client.Tracker().Watch(action.GetResource(), action.GetNamespace())
		if err == nil {
			once.Do(func() { close(started) })
		}
		return true, w, err
	})
	return client, started
}

// loadPolicy loads a policy file the way the provider does at startup.
func loadPolicy(t *testing.T, policy string) *NamespaceAuthorizer {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy.yaml")
	require.NoError(t, os.WriteFile(path, []byte(policy), 0o600))
	authz, err := LoadNamespaceAuthorizer(path)
	require.NoError(t, err)
	return authz
}

func fetch(ctx context.Context, srv *Server, atespace, ns, name string) codes.Code {
	_, err := srv.FetchSecret(ctx, &credproviderpb.FetchSecretRequest{
		Uri:           "ate-secret://google-access-token.k8s.io/default/" + ns + "/" + name + "/credentials.json",
		ActorSpiffeId: "spiffe://substrate-actor.local/actor/" + atespace + "/agent",
	})
	return status.Code(err)
}

// tenantFixture is a provider over a cluster of tenant namespaces: org-a,
// org-b and ate-golden labeled as tenants, platform and org-c not, and a
// Secret in each of them and in org-d, which does not exist yet.
type tenantFixture struct {
	client *fake.Clientset
	srv    *Server
}

func newTenantFixture(t *testing.T, policy string) *tenantFixture {
	t.Helper()
	tenant := map[string]string{tenantLabel: "a"}
	client, watchStarted := watchedClient(
		namespace("platform", nil), namespace("org-a", tenant), namespace("org-b", map[string]string{tenantLabel: "b"}),
		namespace("org-c", nil), namespace("ate-golden", tenant),
		keySecret("platform"), keySecret("org-a"), keySecret("org-b"), keySecret("org-c"), keySecret("org-d"),
		keySecret("ate-golden"),
	)
	authz := loadPolicy(t, policy)
	require.NoError(t, authz.WatchNamespaces(t.Context(), client))
	select {
	case <-watchStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("the namespace watch never opened")
	}
	return &tenantFixture{client: client, srv: NewServer(client, authz, DefaultExchangeTimeout)}
}

func (f *tenantFixture) expect(t *testing.T, atespace, ns string, want codes.Code) {
	t.Helper()
	require.Equal(t, want, fetch(t.Context(), f.srv, atespace, ns, "vertex"), "atespace %s reading %s", atespace, ns)
}

func (f *tenantFixture) eventually(t *testing.T, atespace, ns string, want codes.Code) {
	t.Helper()
	require.Eventually(t, func() bool {
		return fetch(t.Context(), f.srv, atespace, ns, "vertex") == want
	}, 10*time.Second, 10*time.Millisecond, "atespace %s reading %s never answered %v", atespace, ns, want)
}

func (f *tenantFixture) relabel(t *testing.T, name string, labels map[string]string) {
	t.Helper()
	_, err := f.client.CoreV1().Namespaces().Update(t.Context(), namespace(name, labels), metav1.UpdateOptions{})
	require.NoError(t, err)
}

// A namespace the selector matches is readable by the atespace of its own name
// and by no other, and the match follows the namespace's labels after startup.
func TestFetchSecretOwnNamespaceSelector(t *testing.T) {
	f := newTenantFixture(t, tenantPolicy)

	t.Run("a matched namespace reads its own Secrets", func(t *testing.T) {
		f.expect(t, "org-a", "org-a", granted)
	})
	t.Run("a matched namespace cannot read another tenant's", func(t *testing.T) {
		before := len(f.client.Actions())
		f.expect(t, "org-a", "org-b", codes.PermissionDenied)
		require.False(t, slices.ContainsFunc(f.client.Actions()[before:], func(a k8stesting.Action) bool {
			return a.GetResource().Resource == "secrets"
		}), "a cross-tenant request reached the Secrets API")
	})
	t.Run("a static atespace gains no matched namespace", func(t *testing.T) {
		f.expect(t, "platform", "platform", granted)
		f.expect(t, "platform", "org-a", codes.PermissionDenied)
	})
	t.Run("the golden atespace gains no matched namespace, its own name included", func(t *testing.T) {
		f.expect(t, "ate-golden", "org-a", codes.PermissionDenied)
		f.expect(t, "ate-golden", "ate-golden", codes.PermissionDenied)
	})
	t.Run("an unlabeled namespace is not granted", func(t *testing.T) {
		f.expect(t, "org-c", "org-c", codes.PermissionDenied)
	})
	t.Run("a namespace labeled after startup is granted", func(t *testing.T) {
		f.relabel(t, "org-c", map[string]string{tenantLabel: "c"})
		f.eventually(t, "org-c", "org-c", granted)
	})
	t.Run("a namespace created after startup is granted", func(t *testing.T) {
		_, err := f.client.CoreV1().Namespaces().Create(t.Context(), namespace("org-d", map[string]string{tenantLabel: "d"}), metav1.CreateOptions{})
		require.NoError(t, err)
		f.eventually(t, "org-d", "org-d", granted)
	})
	t.Run("a namespace whose label is removed is refused", func(t *testing.T) {
		f.relabel(t, "org-a", nil)
		f.eventually(t, "org-a", "org-a", codes.PermissionDenied)
	})
	t.Run("a deleted namespace is refused", func(t *testing.T) {
		require.NoError(t, f.client.CoreV1().Namespaces().Delete(t.Context(), "org-b", metav1.DeleteOptions{}))
		f.eventually(t, "org-b", "org-b", codes.PermissionDenied)
	})
}

// An allowedNamespaceSelector grants its atespace every namespace it matches,
// following their labels after startup, and grants no other atespace anything.
func TestFetchSecretAllowedNamespaceSelector(t *testing.T) {
	f := newTenantFixture(t, goldenPolicy)

	t.Run("the atespace reads every matched namespace", func(t *testing.T) {
		f.expect(t, "ate-golden", "org-a", granted)
		f.expect(t, "ate-golden", "org-b", granted)
	})
	t.Run("the namespaces it lists stay granted", func(t *testing.T) {
		f.expect(t, "ate-golden", "platform", granted)
	})
	t.Run("an unlabeled namespace is not granted", func(t *testing.T) {
		f.expect(t, "ate-golden", "org-c", codes.PermissionDenied)
	})
	t.Run("a matched namespace gains nothing from another atespace's selector", func(t *testing.T) {
		f.expect(t, "org-a", "org-a", codes.PermissionDenied)
		f.expect(t, "org-a", "org-b", codes.PermissionDenied)
	})
	t.Run("a namespace labeled after startup is granted", func(t *testing.T) {
		f.relabel(t, "org-c", map[string]string{tenantLabel: "c"})
		f.eventually(t, "ate-golden", "org-c", granted)
	})
	t.Run("a namespace whose label is removed is refused", func(t *testing.T) {
		f.relabel(t, "org-a", nil)
		f.eventually(t, "ate-golden", "org-a", codes.PermissionDenied)
	})
}

// A policy file without a namespace selector must not need RBAC on
// namespaces: the provider would otherwise fail to start on every install
// that does not grant it.
func TestWatchNamespacesWithoutSelectorReadsNothing(t *testing.T) {
	client := fake.NewClientset()
	client.PrependReactor("*", "namespaces", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("forbidden")
	})
	authz := loadPolicy(t, "policies:\n- atespace: team-a\n  allowedNamespaces: [ns1]\n")
	require.NoError(t, authz.WatchNamespaces(t.Context(), client))
	require.Empty(t, client.Actions(), "a policy without a namespace selector called the API")
}

func TestLoadNamespaceAuthorizerNamespaceSelectors(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
	}{
		{"with atespace", "policies:\n- atespace: team-a\n  ownNamespaceSelector: {matchLabels: {t: a}}\n"},
		{"with allowedNamespaces", "policies:\n- allowedNamespaces: [ns1]\n  ownNamespaceSelector: {matchLabels: {t: a}}\n"},
		{"empty selector", "policies:\n- ownNamespaceSelector: {}\n"},
		{"unknown operator", "policies:\n- ownNamespaceSelector: {matchExpressions: [{key: t, operator: Present}]}\n"},
		{"invalid key", "policies:\n- ownNamespaceSelector: {matchLabels: {'not a key': a}}\n"},
		{"misspelled field", "policies:\n- ownNamespaceSelector: {matchLabel: {t: a}}\n"},
		{"invalid secretSelector", "policies:\n- ownNamespaceSelector: {matchLabels: {t: a}}\n  secretSelector: {matchLabels: {'not a key': v}}\n"},
		{"with allowedNamespaceSelector", "policies:\n- ownNamespaceSelector: {matchLabels: {t: a}}\n  allowedNamespaceSelector: {matchLabels: {t: a}}\n"},
		{"allowedNamespaceSelector without atespace", "policies:\n- allowedNamespaceSelector: {matchLabels: {t: a}}\n"},
		{"empty allowedNamespaceSelector", "policies:\n- atespace: ate-golden\n  allowedNamespaceSelector: {}\n"},
		{"allowedNamespaceSelector with an unknown operator", "policies:\n- atespace: ate-golden\n  allowedNamespaceSelector: {matchExpressions: [{key: t, operator: Present}]}\n"},
		{"misspelled allowedNamespaceSelector field", "policies:\n- atespace: ate-golden\n  allowedNamespaceSelector: {matchLabel: {t: a}}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.policy), 0o600))
			_, err := LoadNamespaceAuthorizer(path)
			require.Error(t, err)
		})
	}
}

// A secretSelector narrows a selected namespace's grant as it narrows a
// listed one.
func TestFetchSecretNamespaceSelectorsNarrowBySecretLabel(t *testing.T) {
	const narrow = "  secretSelector:\n    matchLabels:\n      example.com/credential: \"true\"\n"
	for _, tc := range []struct{ name, policy, atespace string }{
		{"ownNamespaceSelector", tenantPolicy + narrow, "org-a"},
		{"allowedNamespaceSelector", goldenPolicy + narrow, "ate-golden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			labeled := keySecret("org-a")
			labeled.Labels = map[string]string{"example.com/credential": "true"}
			other := keySecret("org-a")
			other.Name = "other"
			client := fake.NewClientset(namespace("org-a", map[string]string{tenantLabel: "a"}), labeled, other)
			authz := loadPolicy(t, tc.policy)
			require.NoError(t, authz.WatchNamespaces(t.Context(), client))
			srv := NewServer(client, authz, DefaultExchangeTimeout)
			require.Equal(t, granted, fetch(t.Context(), srv, tc.atespace, "org-a", "vertex"), "labeled Secret")
			require.Equal(t, codes.PermissionDenied, fetch(t.Context(), srv, tc.atespace, "org-a", "other"), "unlabeled Secret")
		})
	}
}

// The two selectors together, as a platform with tenant namespaces and
// golden actors sets them: neither widens the other.
func TestFetchSecretTenantAndGoldenSelectors(t *testing.T) {
	f := newTenantFixture(t, tenantPolicy+strings.TrimPrefix(goldenPolicy, "policies:\n"))
	f.expect(t, "ate-golden", "org-a", granted)
	f.expect(t, "ate-golden", "ate-golden", granted)
	f.expect(t, "ate-golden", "org-c", codes.PermissionDenied)
	f.expect(t, "org-a", "org-a", granted)
	f.expect(t, "org-a", "org-b", codes.PermissionDenied)
	f.expect(t, "org-a", "platform", codes.PermissionDenied)
}
