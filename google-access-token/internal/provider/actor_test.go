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
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActorAtespace(t *testing.T) {
	for _, tc := range []struct {
		id      string
		want    string
		wantErr bool
	}{
		{id: "spiffe://substrate-actor.local/actor/team-a/agent-1", want: "team-a"},
		{id: "", wantErr: true},
		{id: "spiffe://cluster.local/actor/team-a/agent-1", wantErr: true},
		{id: "spiffe://substrate-actor.local/ateom-for-actor/team-a/agent-1", wantErr: true},
		{id: "spiffe://substrate-actor.local/actor/team-a", wantErr: true},
		{id: "spiffe://substrate-actor.local/actor/team-a/agent-1/extra", wantErr: true},
		{id: "spiffe://substrate-actor.local/actor/Team_A/agent-1", wantErr: true},
		{id: "spiffe://substrate-actor.local/actor/team-a/agent-1?x=y", wantErr: true},
		{id: "spiffe://user@substrate-actor.local/actor/team-a/agent-1", wantErr: true},
		{id: "https://substrate-actor.local/actor/team-a/agent-1", wantErr: true},
	} {
		got, err := actorAtespace(tc.id)
		if tc.wantErr {
			require.Error(t, err, "actorAtespace(%q)", tc.id)
			continue
		}
		require.NoError(t, err, "actorAtespace(%q)", tc.id)
		require.Equal(t, tc.want, got, "actorAtespace(%q)", tc.id)
	}
}
