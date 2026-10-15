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

// Package provider implements substrate's CredentialProvider API for the
// google-access-token.k8s.io class: a Kubernetes Secret entry holding a Google
// service account key resolves to an OAuth 2.0 access token minted from it. The
// key stays in the cluster; the egress gateway and the actor only ever see a
// token that expires within the hour.
package provider

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/agent-substrate/substrate/pkg/proto/credproviderpb"
)

// Server implements credproviderpb.CredentialProviderServer over the Kubernetes
// API. mTLS admits only the egress gateway, which names each request's actor; a
// Secret resolves only in a namespace the policy grants that actor's atespace.
type Server struct {
	credproviderpb.UnimplementedCredentialProviderServer

	client kubernetes.Interface
	// nsAuth restricts which namespaces an atespace may resolve Secrets from.
	nsAuth *NamespaceAuthorizer
	// google mints and caches the access tokens.
	google *googleTokenExchanger
}

// NewServer builds a provider with a default-deny policy (a nil authorizer
// refuses everything). Each token exchange gets at most exchangeTimeout.
func NewServer(client kubernetes.Interface, nsAuth *NamespaceAuthorizer, exchangeTimeout time.Duration) *Server {
	return &Server{
		client: client,
		nsAuth: nsAuth,
		google: newGoogleTokenExchanger(&http.Client{Timeout: exchangeTimeout}, exchangeTimeout),
	}
}

// FetchSecret resolves one ate-secret:// URI to an access token minted from the
// service account key the named Secret entry holds.
func (s *Server) FetchSecret(ctx context.Context, req *credproviderpb.FetchSecretRequest) (*credproviderpb.FetchSecretResponse, error) {
	ref, err := ParseURI(req.GetUri())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	atespace, err := s.authorize(ctx, req.GetActorSpiffeId(), ref.Namespace)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "resolving credential",
		slog.String("provider", ProviderName),
		slog.String("namespace", ref.Namespace),
		slog.String("secret", ref.Name),
		slog.String("actor", req.GetActorSpiffeId()),
	)

	secret, err := s.client.CoreV1().Secrets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Authorization is enforced, so a missing Secret must look the same
			// as one that fails the label check below: otherwise a caller could
			// walk a list of names and learn what exists here.
			return nil, status.Errorf(codes.PermissionDenied, "atespace %q is not permitted to resolve secret %s/%s", atespace, ref.Namespace, ref.Name)
		}
		if k8serrors.IsForbidden(err) {
			return nil, status.Errorf(codes.PermissionDenied, "not permitted to read secret %s/%s", ref.Namespace, ref.Name)
		}
		return nil, status.Error(codes.Unavailable, "could not read secret from Kubernetes")
	}

	// The Secret is in hand, so a grant narrowed by label can be settled. This
	// refusal and the NotFound above give the same code and message, so a
	// caller learns nothing from the difference.
	if !s.nsAuth.AllowedSecret(atespace, ref.Namespace, secret.GetLabels()) {
		slog.WarnContext(ctx, "credential request denied: secret does not match the grant",
			slog.String("atespace", atespace),
			slog.String("namespace", ref.Namespace),
			slog.String("secret", ref.Name),
		)
		return nil, status.Errorf(codes.PermissionDenied, "atespace %q is not permitted to resolve secret %s/%s", atespace, ref.Namespace, ref.Name)
	}

	key, ok := secret.Data[ref.Key]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "secret %s/%s: key %q not present", ref.Namespace, ref.Name, ref.Key)
	}
	token, err := s.google.AccessToken(ctx, key)
	if err != nil {
		return nil, status.Errorf(status.Code(err), "secret %s/%s: %s", ref.Namespace, ref.Name, status.Convert(err).Message())
	}
	return &credproviderpb.FetchSecretResponse{OpaqueBytes: []byte(token)}, nil
}

// authorize checks the actor identity and its namespace grant before reading
// from Kubernetes. FetchSecret then checks the Secret against any label selector.
func (s *Server) authorize(ctx context.Context, actorSpiffeID, namespace string) (string, error) {
	atespace, err := actorAtespace(actorSpiffeID)
	if err != nil {
		slog.WarnContext(ctx, "credential request denied: unusable actor identity", slog.Any("err", err))
		return "", status.Errorf(codes.PermissionDenied, "actor identity is required and must be a valid actor SPIFFE URI: %v", err)
	}
	if !s.nsAuth.Allowed(atespace, namespace) {
		slog.WarnContext(ctx, "credential request denied: atespace not permitted for namespace",
			slog.String("atespace", atespace), slog.String("namespace", namespace))
		return "", status.Errorf(codes.PermissionDenied, "atespace %q is not permitted to resolve secrets in namespace %q", atespace, namespace)
	}
	return atespace, nil
}
