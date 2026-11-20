// The root holds no Go code: each provider is a module of its own under its
// directory, joined by go.work so the root targets cover them all. The root
// pins the tools the providers' targets run.
module github.com/giantswarm/substrate-credential-providers

go 1.27.2

require (
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/telemetry v0.0.0-20261116191925-e77a43154acb // indirect
	golang.org/x/tools v0.53.0 // indirect
	golang.org/x/vuln v1.10.0 // indirect
)

tool golang.org/x/vuln/cmd/govulncheck
