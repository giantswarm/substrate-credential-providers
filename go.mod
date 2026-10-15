// The root holds no Go code: each provider is a module of its own under its
// directory, joined by go.work so the root targets cover them all. The root
// pins the tools the providers' targets run.
module github.com/giantswarm/substrate-credential-providers

go 1.27.2

require (
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/telemetry v0.0.0-20261010193011-f71dc92da45e // indirect
	golang.org/x/tools v0.52.0 // indirect
	golang.org/x/vuln v1.9.0 // indirect
)

tool golang.org/x/vuln/cmd/govulncheck
