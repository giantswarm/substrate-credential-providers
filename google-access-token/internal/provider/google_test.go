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
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/agent-substrate/substrate/pkg/proto/credproviderpb"
)

const (
	googleTestActor = "spiffe://substrate-actor.local/actor/team-a/vertex"
	googleTestEmail = "vertex@project.iam.gserviceaccount.com"
	googleTestURI   = "ate-secret://google-access-token.k8s.io/default/ns1/vertex/credentials.json"
)

// fakeTokenEndpoint stands in for Google's token endpoint: it verifies each
// assertion against the service account's public key and records its claims.
type fakeTokenEndpoint struct {
	*httptest.Server
	public    *rsa.PublicKey
	hits      atomic.Int32
	expiresIn int
	// respond, when set, replaces the success response.
	respond func(w http.ResponseWriter, hit int32)

	mu     sync.Mutex
	claims []*assertionClaims
}

// assertionClaims are the JWT bearer claims the exchange is expected to sign.
type assertionClaims struct {
	Iss   string `json:"iss"`
	Scope string `json:"scope"`
	Aud   string `json:"aud"`
}

// verifyAssertion checks an RS256 JWT assertion against the service account's
// public key and returns its claims.
func verifyAssertion(assertion string, public *rsa.PublicKey) (*assertionClaims, error) {
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed assertion")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(public, crypto.SHA256, digest[:], signature); err != nil {
		return nil, err
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims assertionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func newFakeTokenEndpoint(t *testing.T, public *rsa.PublicKey) *fakeTokenEndpoint {
	t.Helper()
	f := &fakeTokenEndpoint{public: public, expiresIn: 3600}
	f.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit := f.hits.Add(1)
		if err := r.ParseForm(); err != nil || r.Method != http.MethodPost || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			http.Error(w, `{"error":"unsupported_grant_type"}`, http.StatusBadRequest)
			return
		}
		claims, err := verifyAssertion(r.PostForm.Get("assertion"), f.public)
		if err != nil {
			http.Error(w, `{"error":"invalid_grant","error_description":"Invalid JWT Signature."}`, http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.claims = append(f.claims, claims)
		f.mu.Unlock()
		if f.respond != nil {
			f.respond(w, hit)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"ya29.token-%d","token_type":"Bearer","expires_in":%d}`, hit, f.expiresIn)
	}))
	t.Cleanup(f.Close)
	return f
}

func generateRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	return key
}

// serviceAccountKeyJSON renders a key file of the shape Google issues.
func serviceAccountKeyJSON(t *testing.T, key *rsa.PrivateKey, email, tokenURL string) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	raw, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "project",
		"private_key_id": "key-1",
		"private_key":    string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email":   email,
		"token_uri":      tokenURL,
	})
	require.NoError(t, err)
	return raw
}

func vertexSecret(key []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vertex", Namespace: "ns1"},
		Data:       map[string][]byte{"credentials.json": key},
	}
}

// newGoogleTestServer grants team-a its namespace and points the exchanger at
// the fake endpoint's TLS client.
func newGoogleTestServer(endpoint *fakeTokenEndpoint, secrets ...*corev1.Secret) (*Server, *fake.Clientset) {
	objects := make([]runtime.Object, 0, len(secrets))
	for _, secret := range secrets {
		objects = append(objects, secret)
	}
	client := fake.NewSimpleClientset(objects...)
	srv := NewServer(client, &NamespaceAuthorizer{allowed: map[string]map[string][]grant{"team-a": {"ns1": {{}}}}}, DefaultExchangeTimeout)
	srv.google = newGoogleTokenExchanger(endpoint.Client(), DefaultExchangeTimeout)
	return srv, client
}

func fetchGoogleToken(t *testing.T, srv *Server, uri string) (string, error) {
	t.Helper()
	resp, err := srv.FetchSecret(t.Context(), &credproviderpb.FetchSecretRequest{Uri: uri, ActorSpiffeId: googleTestActor})
	if err != nil {
		return "", err
	}
	return string(resp.GetOpaqueBytes()), nil
}

func TestFetchSecretGoogleAccessToken(t *testing.T) {
	key := generateRSAKey(t)
	endpoint := newFakeTokenEndpoint(t, &key.PublicKey)
	raw := serviceAccountKeyJSON(t, key, googleTestEmail, endpoint.URL)
	srv, _ := newGoogleTestServer(endpoint, vertexSecret(raw))

	token, err := fetchGoogleToken(t, srv, googleTestURI)
	require.NoError(t, err)
	require.Equal(t, "ya29.token-1", token)
	require.NotContains(t, token, "PRIVATE KEY", "response carries the private key")

	again, err := fetchGoogleToken(t, srv, googleTestURI)
	require.NoError(t, err)
	require.Equal(t, token, again, "second fetch must serve the cached token")

	_, err = fetchGoogleToken(t, srv, "ate-secret://google-access-token.k8s.io/default/ns1/vertex")
	require.Equal(t, codes.InvalidArgument, status.Code(err), "the key is required")
	require.EqualValues(t, 1, endpoint.hits.Load(), "want one exchange for two fetches")

	endpoint.mu.Lock()
	claims := endpoint.claims[0]
	endpoint.mu.Unlock()
	require.Equal(t, googleTestEmail, claims.Iss)
	require.Equal(t, googleCloudPlatformScope, claims.Scope)
	require.Equal(t, endpoint.URL, claims.Aud)

	// This provider serves one class; the reference provider's name is refused.
	_, err = fetchGoogleToken(t, srv, "ate-secret://k8s.io/default/ns1/vertex/credentials.json")
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

// The cache serves a token while fifteen minutes remain and exchanges the key
// again before that, so the gateway's five-minute cache never holds an expired
// token. The clock is synctest's; the token endpoint answers in-process, so
// nothing leaves the bubble.
func TestGoogleAccessTokenRefreshesBeforeGatewayCacheOutlivesIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key := generateRSAKey(t)
		raw := serviceAccountKeyJSON(t, key, googleTestEmail, "https://oauth2.example.test/token")
		srv, _ := newGoogleTestServerWithTransport(t, vertexSecret(raw), 3600)

		first, err := fetchGoogleToken(t, srv, googleTestURI)
		require.NoError(t, err)
		require.Equal(t, "ya29.token-1", first)

		time.Sleep(30 * time.Minute)
		cached, err := fetchGoogleToken(t, srv, googleTestURI)
		require.NoError(t, err)
		require.Equal(t, first, cached, "at 30m the token has 30m left and is served from the cache")

		time.Sleep(20 * time.Minute)
		refreshed, err := fetchGoogleToken(t, srv, googleTestURI)
		require.NoError(t, err)
		require.Equal(t, "ya29.token-2", refreshed, "at 50m the token has 10m left and is exchanged again")
	})
}

// A rotated key is a new cache entry; the entry of the old key is dropped once
// it has expired, so rotated keys do not accumulate.
func TestGoogleAccessTokenDropsExpiredEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key := generateRSAKey(t)
		secret := vertexSecret(serviceAccountKeyJSON(t, key, googleTestEmail, "https://oauth2.example.test/token"))
		srv, client := newGoogleTestServerWithTransport(t, secret, 3600)

		_, err := fetchGoogleToken(t, srv, googleTestURI)
		require.NoError(t, err)
		require.Len(t, srv.google.tokens, 1)

		secret.Data["credentials.json"] = serviceAccountKeyJSON(t, key, "rotated@project.iam.gserviceaccount.com", "https://oauth2.example.test/token")
		_, err = client.CoreV1().Secrets("ns1").Update(t.Context(), secret, metav1.UpdateOptions{})
		require.NoError(t, err)
		_, err = fetchGoogleToken(t, srv, googleTestURI)
		require.NoError(t, err)
		require.Len(t, srv.google.tokens, 2, "the old key's token is kept until it expires")

		time.Sleep(61 * time.Minute)
		_, err = fetchGoogleToken(t, srv, googleTestURI)
		require.NoError(t, err)
		require.Len(t, srv.google.tokens, 1, "the old key's expired token is dropped on the next fetch")
	})
}

// roundTripFunc is an http.RoundTripper over a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newGoogleTestServerWithTransport points the exchanger at a token endpoint
// answered in-process: numbered tokens living expiresIn seconds, for tests
// inside a synctest bubble, where nothing may leave the process.
func newGoogleTestServerWithTransport(t *testing.T, secret *corev1.Secret, expiresIn int) (*Server, *fake.Clientset) {
	t.Helper()
	client := fake.NewSimpleClientset(secret)
	srv := NewServer(client, &NamespaceAuthorizer{allowed: map[string]map[string][]grant{"team-a": {"ns1": {{}}}}}, DefaultExchangeTimeout)
	var hits int
	srv.google = newGoogleTokenExchanger(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if err := r.ParseForm(); err != nil {
			return nil, err
		}
		claims, err := unverifiedClaims(r.PostForm.Get("assertion"))
		if err != nil {
			return nil, err
		}
		require.Equal(t, googleCloudPlatformScope, claims.Scope)
		hits++
		body := fmt.Sprintf(`{"access_token":"ya29.token-%d","token_type":"Bearer","expires_in":%d}`, hits, expiresIn)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}, DefaultExchangeTimeout)
	return srv, client
}

// unverifiedClaims decodes an assertion's claims without checking its
// signature, which the TLS endpoint tests cover.
func unverifiedClaims(assertion string) (*assertionClaims, error) {
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed assertion")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims assertionClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

func TestGoogleAccessTokenSerializesConcurrentFetches(t *testing.T) {
	key := generateRSAKey(t)
	endpoint := newFakeTokenEndpoint(t, &key.PublicKey)
	endpoint.respond = func(w http.ResponseWriter, hit int32) {
		time.Sleep(50 * time.Millisecond)
		_, _ = fmt.Fprintf(w, `{"access_token":"ya29.token-%d","token_type":"Bearer","expires_in":3600}`, hit)
	}
	srv, _ := newGoogleTestServer(endpoint, vertexSecret(serviceAccountKeyJSON(t, key, googleTestEmail, endpoint.URL)))

	const fetches = 16
	tokens := make([]string, fetches)
	errs := make([]error, fetches)
	var wg sync.WaitGroup
	for i := range fetches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tokens[i], errs[i] = fetchGoogleToken(t, srv, googleTestURI)
		}()
	}
	wg.Wait()
	for i := range fetches {
		require.NoError(t, errs[i], "fetch %d", i)
		require.Equal(t, "ya29.token-1", tokens[i], "fetch %d", i)
	}
	require.EqualValues(t, 1, endpoint.hits.Load(), "want one shared exchange")
}

func TestGoogleAccessTokenFollowsKeyRotation(t *testing.T) {
	key := generateRSAKey(t)
	endpoint := newFakeTokenEndpoint(t, &key.PublicKey)
	secret := vertexSecret(serviceAccountKeyJSON(t, key, googleTestEmail, endpoint.URL))
	srv, client := newGoogleTestServer(endpoint, secret)

	_, err := fetchGoogleToken(t, srv, googleTestURI)
	require.NoError(t, err)
	rotated := "rotated@project.iam.gserviceaccount.com"
	secret.Data["credentials.json"] = serviceAccountKeyJSON(t, key, rotated, endpoint.URL)
	_, err = client.CoreV1().Secrets("ns1").Update(t.Context(), secret, metav1.UpdateOptions{})
	require.NoError(t, err)
	token, err := fetchGoogleToken(t, srv, googleTestURI)
	require.NoError(t, err)
	require.Equal(t, "ya29.token-2", token, "want a token for the rotated key")
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	require.Len(t, endpoint.claims, 2)
	require.Equal(t, rotated, endpoint.claims[1].Iss, "want the second assertion for the rotated key")
}

func TestGoogleAccessTokenErrors(t *testing.T) {
	key := generateRSAKey(t)
	other := generateRSAKey(t)
	for _, tc := range []struct {
		name      string
		secret    func(endpoint *fakeTokenEndpoint) []byte
		respond   func(w http.ResponseWriter, hit int32)
		expiresIn int
		code      codes.Code
		contains  string
		hits      int32
	}{
		{
			name:     "not a key file",
			secret:   func(*fakeTokenEndpoint) []byte { return []byte("static-token") },
			code:     codes.FailedPrecondition,
			contains: "not a Google service account key",
		},
		{
			name:     "user credential",
			secret:   func(*fakeTokenEndpoint) []byte { return []byte(`{"type":"authorized_user","client_id":"x"}`) },
			code:     codes.FailedPrecondition,
			contains: "authorized_user",
		},
		{
			name: "plaintext token endpoint",
			secret: func(endpoint *fakeTokenEndpoint) []byte {
				return serviceAccountKeyJSON(t, key, googleTestEmail, "http://"+strings.TrimPrefix(endpoint.URL, "https://"))
			},
			code:     codes.FailedPrecondition,
			contains: "https",
		},
		{
			name: "corrupt private key",
			secret: func(endpoint *fakeTokenEndpoint) []byte {
				raw := serviceAccountKeyJSON(t, key, googleTestEmail, endpoint.URL)
				var fields map[string]string
				require.NoError(t, json.Unmarshal(raw, &fields))
				fields["private_key"] = "-----BEGIN " + "PRIVATE KEY-----\nAAAA\n-----END " + "PRIVATE KEY-----\n"
				raw, err := json.Marshal(fields)
				require.NoError(t, err)
				return raw
			},
			code:     codes.FailedPrecondition,
			contains: "private_key",
		},
		{
			name: "rejected by Google",
			secret: func(endpoint *fakeTokenEndpoint) []byte {
				return serviceAccountKeyJSON(t, other, googleTestEmail, endpoint.URL)
			},
			code:     codes.FailedPrecondition,
			contains: "invalid_grant: Invalid JWT Signature.",
			hits:     1,
		},
		{
			name: "endpoint failure",
			secret: func(endpoint *fakeTokenEndpoint) []byte {
				return serviceAccountKeyJSON(t, key, googleTestEmail, endpoint.URL)
			},
			respond: func(w http.ResponseWriter, _ int32) {
				http.Error(w, "upstream body must stay out of the status", http.StatusServiceUnavailable)
			},
			code: codes.Unavailable,
			hits: 1,
		},
		{
			name: "token too short lived",
			secret: func(endpoint *fakeTokenEndpoint) []byte {
				return serviceAccountKeyJSON(t, key, googleTestEmail, endpoint.URL)
			},
			expiresIn: 60,
			code:      codes.Unavailable,
			contains:  "outlive the gateway cache",
			hits:      1,
		},
		{
			name: "token without expiry",
			secret: func(endpoint *fakeTokenEndpoint) []byte {
				return serviceAccountKeyJSON(t, key, googleTestEmail, endpoint.URL)
			},
			respond: func(w http.ResponseWriter, _ int32) {
				_, _ = fmt.Fprint(w, `{"access_token":"ya29.forever","token_type":"Bearer"}`)
			},
			code: codes.Unavailable,
			hits: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			endpoint := newFakeTokenEndpoint(t, &key.PublicKey)
			endpoint.respond = tc.respond
			if tc.expiresIn != 0 {
				endpoint.expiresIn = tc.expiresIn
			}
			srv, _ := newGoogleTestServer(endpoint, vertexSecret(tc.secret(endpoint)))
			_, err := fetchGoogleToken(t, srv, googleTestURI)
			require.Equal(t, tc.code, status.Code(err), "FetchSecret error = %v", err)
			require.Contains(t, err.Error(), tc.contains)
			require.NotContains(t, err.Error(), "stay out of the status", "token endpoint response body exposed")
			require.EqualValues(t, tc.hits, endpoint.hits.Load(), "token endpoint hits")
		})
	}
}

func TestGoogleAccessTokenDeniedBeforeAnyLookup(t *testing.T) {
	key := generateRSAKey(t)
	endpoint := newFakeTokenEndpoint(t, &key.PublicKey)
	srv, client := newGoogleTestServer(endpoint, vertexSecret(serviceAccountKeyJSON(t, key, googleTestEmail, endpoint.URL)))
	_, err := srv.FetchSecret(t.Context(), &credproviderpb.FetchSecretRequest{
		Uri: googleTestURI, ActorSpiffeId: "spiffe://substrate-actor.local/actor/team-b/vertex",
	})
	require.Equal(t, codes.PermissionDenied, status.Code(err), "FetchSecret error = %v", err)
	require.Empty(t, client.Actions(), "denied request reached Kubernetes")
	require.Zero(t, endpoint.hits.Load(), "denied request reached the token endpoint")
}

// A caller waiting behind another's exchange of the same key gives up when its
// own context ends, instead of queueing past the gateway's deadline.
func TestGoogleAccessTokenWaiterHonorsItsContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		key := generateRSAKey(t)
		secret := vertexSecret(serviceAccountKeyJSON(t, key, googleTestEmail, "https://oauth2.example.test/token"))
		srv, _ := newGoogleTestServerWithTransport(t, secret, 3600)
		release := make(chan struct{})
		transport := srv.google.client.Transport
		srv.google.client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
			return transport.RoundTrip(r)
		})

		first := make(chan error, 1)
		go func() {
			_, err := fetchGoogleToken(t, srv, googleTestURI)
			first <- err
		}()
		synctest.Wait()

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		_, err := srv.FetchSecret(ctx, &credproviderpb.FetchSecretRequest{Uri: googleTestURI, ActorSpiffeId: googleTestActor})
		require.Equal(t, codes.DeadlineExceeded, status.Code(err), "FetchSecret error = %v", err)

		close(release)
		require.NoError(t, <-first)
	})
}

// A key Google rejects leaves no cache entry behind once another fetch runs.
func TestGoogleAccessTokenDropsEntriesOfRejectedKeys(t *testing.T) {
	key := generateRSAKey(t)
	endpoint := newFakeTokenEndpoint(t, &key.PublicKey)
	secret := vertexSecret(serviceAccountKeyJSON(t, generateRSAKey(t), googleTestEmail, endpoint.URL))
	srv, client := newGoogleTestServer(endpoint, secret)

	_, err := fetchGoogleToken(t, srv, googleTestURI)
	require.Equal(t, codes.FailedPrecondition, status.Code(err), "FetchSecret error = %v", err)

	secret.Data["credentials.json"] = serviceAccountKeyJSON(t, key, googleTestEmail, endpoint.URL)
	_, err = client.CoreV1().Secrets("ns1").Update(t.Context(), secret, metav1.UpdateOptions{})
	require.NoError(t, err)
	_, err = fetchGoogleToken(t, srv, googleTestURI)
	require.NoError(t, err)
	require.Len(t, srv.google.tokens, 1, "the rejected key's entry is dropped")
}
