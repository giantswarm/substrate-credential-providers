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

// Command google-access-token-credential-provider serves substrate's
// CredentialProvider gRPC API for the google-access-token.k8s.io class: a
// Kubernetes Secret entry holding a Google service account key resolves to an
// access token minted from it. The egress gateway calls it to inject the token
// into an actor's Vertex AI requests; it is the only component in that path
// that reads the key.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sync/atomic"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/agent-substrate/substrate/pkg/proto/credproviderpb"

	"github.com/giantswarm/substrate-credential-providers/google-access-token/internal/mtls"
	"github.com/giantswarm/substrate-credential-providers/google-access-token/internal/provider"
)

var (
	listenAddr   = flag.String("listen-address", ":50051", "gRPC listen address")
	healthAddr   = flag.String("health-address", ":9090", "HTTP listen address for /healthz and /readyz")
	serverBundle = flag.String("server-cred-bundle", "", "credential bundle (PKCS#8 key and certificate chain) presented for serving TLS (required)")
	clientCAFile = flag.String("client-ca-file", "", "trust bundle the caller's client certificate must chain to (required)")
	// No default: the gateway's namespace and ServiceAccount are known only to
	// the deployment.
	injectorIdentity = flag.String("injector-identity", "", "SPIFFE ID the caller's client certificate must carry: substrate's egress gateway, e.g. spiffe://<trust-domain>/ns/<namespace>/sa/<release>-atenet-egress (required)")
	nsPolicyFile     = flag.String("namespace-policy-file", "", "path to the atespace-to-namespace authorization YAML (required)")
	logLevel         = flag.String("log-level", "info", "one of debug, info, warn, error")
	drainGrace       = flag.Duration("drain-grace", 5*time.Second, "how long to wait for in-flight RPCs on shutdown before a hard stop")
	exchangeTimeout  = flag.Duration("exchange-timeout", provider.DefaultExchangeTimeout, "how long one exchange of a service account key at Google's token endpoint may take")
	kubeAPIQPS       = flag.Float64("kube-api-qps", 50, "sustained queries per second allowed against the Kubernetes API")
	kubeAPIBurst     = flag.Int("kube-api-burst", 100, "burst queries allowed against the Kubernetes API")
)

// version is set at build time with -ldflags "-X main.version=v0.1.0".
var version string

func main() {
	flag.Parse()

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevel)); err != nil {
		fmt.Fprintf(os.Stderr, "invalid --log-level %q: %v\n", *logLevel, err)
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	slog.Info("starting google-access-token-credential-provider", slog.String("version", buildVersion()))
	if err := run(context.Background()); err != nil {
		slog.Error("google-access-token-credential-provider exited with error", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if *serverBundle == "" {
		return errors.New("--server-cred-bundle is required")
	}
	if *clientCAFile == "" {
		return errors.New("--client-ca-file is required")
	}
	if *injectorIdentity == "" {
		return errors.New("--injector-identity is required")
	}
	if *nsPolicyFile == "" {
		return errors.New("--namespace-policy-file is required")
	}
	if *exchangeTimeout <= 0 {
		return fmt.Errorf("--exchange-timeout must be positive, got %v", *exchangeTimeout)
	}
	if *kubeAPIQPS <= 0 || *kubeAPIBurst <= 0 {
		return errors.New("--kube-api-qps and --kube-api-burst must be positive")
	}
	authz, err := provider.LoadNamespaceAuthorizer(*nsPolicyFile)
	if err != nil {
		return fmt.Errorf("namespace policy: %w", err)
	}
	slog.Info("loaded namespace authorization policy", slog.String("file", *nsPolicyFile))

	creds, err := mtls.ServerCredentials(mtls.Config{
		ServerBundle:   *serverBundle,
		ClientCAFile:   *clientCAFile,
		CallerIdentity: *injectorIdentity,
	})
	if err != nil {
		return fmt.Errorf("server credentials: %w", err)
	}
	slog.Info("admitting callers", slog.String("client_ca", *clientCAFile), slog.String("required_san", *injectorIdentity))

	client, err := newKubeClient()
	if err != nil {
		return fmt.Errorf("kubernetes client: %w", err)
	}
	if err := authz.WatchNamespaces(ctx, client); err != nil {
		return fmt.Errorf("namespace policy: %w", err)
	}

	srv := grpc.NewServer(grpc.Creds(creds))
	credproviderpb.RegisterCredentialProviderServer(srv, provider.NewServer(client, authz, *exchangeTimeout))

	grpcLis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", *listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *listenAddr, err)
	}
	healthLis, err := (&net.ListenConfig{}).Listen(ctx, "tcp", *healthAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", *healthAddr, err)
	}
	var ready atomic.Bool
	healthSrv := &http.Server{Handler: healthHandler(&ready), ReadHeaderTimeout: 10 * time.Second}
	healthDone := make(chan error, 1)
	go func() { healthDone <- healthSrv.Serve(healthLis) }()

	shutdownCtx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	shutdownErr := make(chan error, 1)
	go func() {
		var err error
		select {
		case <-shutdownCtx.Done():
			slog.Info("shutting down")
		case err = <-healthDone:
			err = fmt.Errorf("health server: %w", err)
		}
		ready.Store(false)
		gracefulStop(srv, *drainGrace)
		_ = healthSrv.Close()
		shutdownErr <- err
	}()

	ready.Store(true)
	slog.Info("google-access-token-credential-provider listening", slog.String("address", grpcLis.Addr().String()), slog.String("health_address", healthLis.Addr().String()))
	// ErrServerStopped means shutdown began before Serve started: not a failure.
	if err := srv.Serve(grpcLis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("serving: %w", err)
	}
	return <-shutdownErr
}

// newKubeClient builds the in-cluster client for the Secrets and, when a policy
// selects namespaces, the namespace watch.
func newKubeClient() (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	cfg.QPS = float32(*kubeAPIQPS)
	cfg.Burst = *kubeAPIBurst
	return kubernetes.NewForConfig(cfg)
}

// gracefulStop lets in-flight RPCs finish for up to grace, then forces the
// server down.
func gracefulStop(srv *grpc.Server, grace time.Duration) {
	done := make(chan struct{})
	go func() {
		srv.GracefulStop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(grace):
		slog.Warn("graceful shutdown timed out; forcing stop", slog.Duration("grace", grace))
		srv.Stop()
	}
}

// healthHandler serves /healthz, which always succeeds so a draining pod is not
// restarted, and /readyz, which succeeds only while ready is set.
func healthHandler(ready *atomic.Bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

// buildVersion returns the release or module version, else "dev", plus the VCS
// revision when the build recorded one.
func buildVersion() string {
	v := version
	info, ok := debug.ReadBuildInfo()
	if !ok {
		if v == "" {
			return "dev"
		}
		return v
	}
	if v == "" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		v = info.Main.Version
	}
	if v == "" {
		v = "dev"
	}
	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if revision == "" {
		return v
	}
	if modified == "true" {
		revision += "-dirty"
	}
	return v + " commit=" + revision
}
