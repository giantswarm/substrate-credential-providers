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
	"regexp"
	"strings"
)

// actorTrustDomain is the trust domain of the actor SPIFFE IDs substrate mints.
const actorTrustDomain = "substrate-actor.local"

// dns1123Label matches a substrate resource name, atespaces included.
var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// actorAtespace returns the atespace of an actor SPIFFE ID,
// spiffe://substrate-actor.local/actor/<atespace>/<name>. It follows substrate's
// internal/resources.ActorRefFromActorSPIFFEID, which this module cannot import.
func actorAtespace(id string) (string, error) {
	const format = "spiffe://" + actorTrustDomain + "/actor/<atespace>/<name>"
	u, err := url.Parse(id)
	if err != nil {
		return "", fmt.Errorf("invalid actor SPIFFE ID %q: %w", id, err)
	}
	if u.Scheme != "spiffe" || u.Host != actorTrustDomain || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%q does not have format %s", id, format)
	}
	segments := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(segments) != 3 || segments[0] != "actor" {
		return "", fmt.Errorf("%q does not have format %s", id, format)
	}
	atespace, name := segments[1], segments[2]
	if !dns1123Label.MatchString(atespace) {
		return "", fmt.Errorf("%q is not a valid atespace", atespace)
	}
	if !dns1123Label.MatchString(name) {
		return "", fmt.Errorf("%q is not a valid actor name", name)
	}
	return atespace, nil
}
