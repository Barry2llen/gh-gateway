# P2-B Release / Packaging Report

## Release trigger

`.github/workflows/release.yml` is started manually with `workflow_dispatch` from the `main` branch. The operator supplies a release tag such as `v0.1.0` or `v0.1.0-rc.1`; the workflow rejects non-semantic tags and tags that already point to another commit. Tests, vet, and Windows binary builds complete before the release tag is created or any image or release is published.

If the requested tag already points to the same dispatched commit, the tag creation step is skipped. This permits a failed run to be retried without moving an existing tag.

The workflow keeps only orchestration and environment wiring in YAML. Release validation, Windows artifact construction, tag creation, and GitHub Release creation are implemented as focused scripts under `scripts/` so they can be syntax-checked and exercised outside GitHub Actions.

The workflow uses only the repository `GITHUB_TOKEN`, with `contents: write` and `packages: write` permissions. It does not run the opt-in Windows administrator E2E because that test intentionally changes the hosts file and certificate trust.

## Published artifacts

Each accepted release publishes these GitHub Release assets:

- `gh-gateway-windows-amd64.exe`
- `gh-gateway-windows-arm64.exe`
- `SHA256SUMS`, covering both executables

The binaries are built with `CGO_ENABLED=0`, `-trimpath`, stripped symbols, and embedded version and commit metadata. No Linux/macOS CLI binaries or container tarballs are attached.

## GHCR tags

The workflow builds the runtime target for `linux/amd64` and `linux/arm64` and pushes a multi-platform manifest to `ghcr.io/barry2llen/gh-gateway`.

For a stable tag such as `v0.1.0`, it publishes:

- `ghcr.io/barry2llen/gh-gateway:v0.1.0`
- `ghcr.io/barry2llen/gh-gateway:0.1`
- `ghcr.io/barry2llen/gh-gateway:latest`

For a prerelease such as `v0.1.0-rc.1`, only the exact `v0.1.0-rc.1` image tag is published. Prereleases do not move the minor or `latest` tags.

## Version embedding and CLI/image binding

`internal/version` defaults development builds to version `dev`, commit `unknown`, and runtime image `ghcr.io/barry2llen/gh-gateway:latest`. Release builds inject the tag and `GITHUB_SHA` with Go linker flags. `gh-gateway version` prints the version and a seven-character commit identifier.

A released CLI uses its exact version as the default runtime image tag. For example, CLI `v0.1.0` selects `ghcr.io/barry2llen/gh-gateway:v0.1.0`. `--image` continues to override that default. Docker creation explicitly uses `--pull missing`, so an absent image is pulled while an existing local image is retained.

The Dockerfile accepts the same `VERSION` and `COMMIT` build arguments, so the executable inside a published runtime image reports matching metadata.

## README installation flow

The primary Windows instructions now start with downloading the appropriate amd64 or arm64 executable from GitHub Releases. Users no longer need to clone the repository, compile Go, or build the runtime image. Docker Desktop remains required, and local-mode mutation commands require an elevated PowerShell terminal.

Source and local Docker builds remain documented separately under Development. The CA lifecycle remains unchanged: `stop` retains the exact installed local CA for reuse, while `uninstall` removes that CA by its recorded thumbprint.

## Validation performed

The following checks passed on Windows 11 with Go 1.27.0 and Docker Desktop:

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `docker build --target runtime .`
- `actionlint` 1.7.7 against `.github/workflows/release.yml`
- Windows amd64 and arm64 cross-builds with test version `v0.0.0-test`
- execution of the amd64 test binary, which reported `v0.0.0-test` and the expected shortened commit
- inspection of `start --help`, which showed `ghcr.io/barry2llen/gh-gateway:v0.0.0-test` as the default image
- versioned runtime image build and execution of `gh-gateway version` inside that image
- no-push Buildx export of a `linux/amd64,linux/arm64` OCI image index

No real release tag was pushed and no GitHub Release or GHCR package was published during validation.

## Known limitations

- The repository owner must ensure the GHCR package is public so an unauthenticated Docker Desktop installation can pull it. Package visibility is an operational GitHub setting and is not changed by this workflow.
- The actual GitHub-hosted manual workflow, GitHub Release creation, and GHCR push can only be fully verified by running a real release.
- Windows Authenticode signing and an installer are not included.
- The existing elevated Windows integration test remains a manual pre-release check and was not rerun for this packaging-only milestone.
- P0/P1/P2-A compatibility behavior and P3 mutations are unchanged.

## Verdict

```text
P2-B Release / Packaging: PASS
```
