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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNamespaceAuthorizer(t *testing.T) {
	authz, err := newNamespaceAuthorizer(namespacePolicyFile{
		Policies: []atespaceNamespacePolicy{
			{Atespace: "team-a", AllowedNamespaces: []string{"ns1", "shared"}},
			{Atespace: "team-b", AllowedNamespaces: []string{"ns2"}},
		},
	})
	require.NoError(t, err)
	for _, tc := range []struct {
		atespace, namespace string
		want                bool
	}{
		{"team-a", "ns1", true},
		{"team-a", "shared", true},
		{"team-a", "ns2", false},
		{"team-b", "ns2", true},
		{"team-c", "ns1", false},
		{"team-a", "", false},
	} {
		require.Equal(t, tc.want, authz.Allowed(tc.atespace, tc.namespace), "Allowed(%q, %q)", tc.atespace, tc.namespace)
	}

	empty, err := newNamespaceAuthorizer(namespacePolicyFile{})
	require.NoError(t, err)
	require.False(t, empty.Allowed("team-a", "ns1"), "an empty policy denies everything")

	var nilAuthz *NamespaceAuthorizer
	require.False(t, nilAuthz.Allowed("team-a", "ns1"), "a nil authorizer denies everything")

	_, err = newNamespaceAuthorizer(namespacePolicyFile{
		Policies: []atespaceNamespacePolicy{{AllowedNamespaces: []string{"ns1"}}},
	})
	require.Error(t, err, "a policy without an atespace is refused")
}

func TestLoadNamespaceAuthorizer(t *testing.T) {
	for _, tc := range []struct {
		name, policy string
		wantErr      bool
	}{
		{name: "valid", policy: "policies:\n- atespace: team-a\n  allowedNamespaces: [ns1]\n"},
		{name: "valid with selector", policy: "policies:\n- atespace: team-a\n  allowedNamespaces: [ns1]\n  secretSelector:\n    matchLabels:\n      example.com/credential: \"true\"\n"},
		{name: "empty", policy: "policies: []"},
		{name: "unknown field", policy: "polices: []", wantErr: true},
		{name: "duplicate field", policy: "policies: []\npolicies: []", wantErr: true},
		{name: "missing atespace", policy: "policies: [{allowedNamespaces: [ns1]}]", wantErr: true},
		{name: "invalid atespace", policy: "policies: [{atespace: Team_A, allowedNamespaces: [ns1]}]", wantErr: true},
		{name: "invalid namespace", policy: "policies: [{atespace: team-a, allowedNamespaces: ['*']}]", wantErr: true},
		{name: "invalid label key", policy: "policies: [{atespace: team-a, allowedNamespaces: [ns1], secretSelector: {matchLabels: {'bad key': x}}}]", wantErr: true},
		{name: "invalid label value", policy: "policies: [{atespace: team-a, allowedNamespaces: [ns1], secretSelector: {matchLabels: {k: 'bad value'}}}]", wantErr: true},
		{name: "malformed", policy: "policies: [", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.policy), 0o600))
			auth, err := LoadNamespaceAuthorizer(path)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.name != "empty", auth.Allowed("team-a", "ns1"))
		})
	}
	_, err := LoadNamespaceAuthorizer(filepath.Join(t.TempDir(), "absent"))
	require.Error(t, err, "a missing policy file is refused")
}

// Grants of one atespace are OR-ed: a Secret is admitted when any one policy
// admits it; within a policy every label of the selector must match.
func TestAllowedSecret(t *testing.T) {
	authz, err := newNamespaceAuthorizer(namespacePolicyFile{
		Policies: []atespaceNamespacePolicy{
			{Atespace: "team-a", AllowedNamespaces: []string{"ns1"},
				SecretSelector: &secretSelector{MatchLabels: map[string]string{"a": "1", "b": "2"}}},
			{Atespace: "team-a", AllowedNamespaces: []string{"ns1"},
				SecretSelector: &secretSelector{MatchLabels: map[string]string{"c": "3"}}},
			{Atespace: "team-b", AllowedNamespaces: []string{"ns1"}},
		},
	})
	require.NoError(t, err)
	require.True(t, authz.AllowedSecret("team-a", "ns1", map[string]string{"a": "1", "b": "2"}))
	require.True(t, authz.AllowedSecret("team-a", "ns1", map[string]string{"c": "3"}))
	require.False(t, authz.AllowedSecret("team-a", "ns1", map[string]string{"a": "1"}), "every label of one selector must match")
	require.False(t, authz.AllowedSecret("team-a", "ns1", nil), "a narrowed grant admits no unlabeled Secret")
	require.True(t, authz.AllowedSecret("team-b", "ns1", nil), "a grant without a selector admits every Secret")
	require.False(t, authz.AllowedSecret("team-b", "ns2", nil))
}
