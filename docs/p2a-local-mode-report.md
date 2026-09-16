# P2-A Windows Local Transparent Mode Report

## Architecture and commands

The Windows CLI exposes `start`, `status`, `doctor`, `stop`, and `uninstall`; `serve` remains the container/server entry point and the no-subcommand default. Local mode runs one Linux container bound to IPv4 localhost. `/api/graphql` and `/api/v3/*` retain the existing compatibility handlers, while all other HTTPS traffic uses Go's reverse proxy.

Local orchestration is concentrated in `internal/localmode`. Hosts, certificate trust, Docker CLI, DNS/TLS resolution, ports, state, elevation, and locking are replaceable seams so default tests do not modify the developer machine.

## State, TLS, and hosts

`%LOCALAPPDATA%\gh-gateway\state.json` is versioned and atomically replaced. It records the instance, host, original IPv4 address, ports, container identity, image, hosts state, CA thumbprint, and active state. It never records a Gitea token or Authorization header.

The first start creates an ECDSA P-256 CA named `gh-gateway Local CA`, installs it into the Windows Current User Root store, and creates a 397-day hostname certificate. CA removal uses only the saved SHA-1 thumbprint. Private-key ACL inheritance is removed and access is granted to the current user. Docker receives only the leaf certificate and leaf key through `docker cp` before startup; the CA key never enters the container or image, and the runtime root filesystem is read-only. This avoids Docker Desktop bind-mount visibility failures under `%LOCALAPPDATA%`.

The hosts editor owns only instance-specific `# gh-gateway begin/end` blocks. It rejects pre-existing mappings for the host, preserves unrelated content and line endings, and replaces the file through a flushed same-directory temporary file.

## Anti-loop, proxy, SSH, and rollback

Before writing hosts, start resolves IPv4 candidates and selects one that completes normal TLS validation with the configured hostname. Compatibility and ordinary proxy requests connect to that address while retaining the configured HTTP Host and TLS SNI. Certificate verification remains enabled. A different incoming Host receives HTTP 421.

Optional SSH support publishes the configured port and performs raw bidirectional TCP copying to the recorded upstream IP, with half-close after each completed direction. `--no-ssh-proxy` leaves SSH unpublished and prints a warning.

Start persists reusable inactive CA metadata, starts and directly probes the container, installs hosts, validates through the system resolver/trust store, and only then marks state active. Failure removes the marker and container in reverse order while retaining the CA. Stop is idempotent. Status derives `running`, `stopped`, or `inconsistent` from state, hosts, Docker publication, trust, and leaf validity.

## Security boundaries

- No token is persisted, printed, or supplied to the container.
- Upstream TLS verification is never disabled.
- Host validation prevents arbitrary proxying.
- CA removal never searches or deletes by subject.
- Local mutations require an elevated Windows terminal; automatic elevation is not attempted.
- Failed-only rerun and run-level ZIP logs remain unsupported.

## Tests and verdict

Automated tests cover hosts ownership/idempotency, state replacement, certificate SANs, Docker arguments without credentials, Host rejection/fallback, elevation rejection, and rollback. The opt-in administrator test is `scripts/windows-local-e2e.ps1`; its `finally` block calls stop and verifies cleanup.

Verified in this implementation run:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- Linux runtime image build
- containerized unit/race/vet profile
- the existing full Docker Compose P0/P1-A/P1-B E2E, including both gh versions, protocol fixtures, real Gitea Actions, and the explicit failed-only rerun unsupported response
- the opt-in elevated Windows E2E against `git.yinlihupo.cn`, including hosts override, trusted local TLS, ordinary passthrough, `gh api user`, `gh repo view`, `gh pr view`, running status, container removal, and hosts restoration

Only Windows 11, Docker Desktop, IPv4 localhost, one Gitea host, HTTPS 443, and optional same-port SSH are supported. Automatic UAC, services, multi-host operation, macOS/Linux orchestration, DNS, WSL-specific networking, Kubernetes, installers, and packaging are outside P2-A.

The elevated Windows integration test completed successfully and its `finally` cleanup returned the runtime to stopped state:

```text
Windows local transparent mode: PASS
```
