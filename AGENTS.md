# Repository instructions

## Before finishing Go changes

CI (`.github/workflows/build.yaml`) runs a fixed set of checks. Run `scripts/linting.sh`
(same checks; `--fix` rewrites imports first) and fix everything before calling Go work
done. CI runs this same script, so it is the single place to add or change a check. While
iterating you can scope `golangci-lint run` to the touched packages.

`golangci-lint` (config: `.golangci.yml`) enforces errcheck, ineffassign, revive,
staticcheck, unused, misspell, modernize, plus gofmt and goimports. Details that bite:

- goimports uses `local-prefixes: github.com/nversion/agent-gateway-token-helper`: imports
  go in three groups - stdlib, third-party, then this module - each separated by a blank
  line. Format with `goimports -local github.com/nversion/agent-gateway-token-helper -w <files>`.
- Unchecked errors: in tests use `defer func() { _ = w.Close() }()`.
- `internal/store`, `internal/lock` and `internal/browser` have per-OS files. The script
  vets and lints with `GOOS=windows` and `GOOS=linux` too; the cgo Keychain code
  (`keychain_darwin.go`) is only compiled on macOS.

## Dependencies

Runtime dependencies use only the standard library and Go-team modules (`golang.org/x/...`).
No go-oidc or JWT libraries: ID token claims are decoded in `internal/idtoken` (see its
package doc for why signatures aren't verified). Development tools such as staticcheck,
and their transitive dependencies in `go.mod`, are exempt from this restriction.

## Tests

- `go test ./...` - unit tests, including in-process flows against `internal/oidctest`.
- `go test -tags integration ./test/...` - builds `agw-token` and runs it as a subprocess
  against the fake provider, playing the browser by following the `Open this URL:` line.
- `AGW_TEST_NATIVE_STORE=1 go test ./internal/store` - round-trips through the real
  Keychain / DPAPI / Secret Service. Opt-in because it touches the user's secret store.

## Building

`scripts/build.sh <goos> <goarch>` is the only place build flags live; CI's build job
calls it. It pins the exact Go version from the `go` line in `go.mod`, so bumping that
line changes every release hash. Linux and Windows builds must stay byte-for-byte
reproducible (the `reproducible` CI job builds them twice and diffs): no cgo, no VCS
stamping, nothing time- or path-dependent in ldflags.

Never change the `UpgradeCode` in `packaging/windows/agw-token.wxs`: it is how a newer
MSI finds and replaces an installed one.

## Documentation

`README.md` is a short intro; link to other files with relative paths. Harness setup lives in `docs/` (one file
per harness), configuration reference in `docs/configuration.md`, and building, CI,
verification, releasing and signing in `DEVELOPMENT.md`.

`docs/` is also the GitHub Pages site (https://agw-token.nversion.dk/),
published by `.github/workflows/pages.yaml` on every push to `main` that touches it.
`docs/index.html` is the setup generator: a single file with no external scripts, fonts or
build step, so the workflow uploads `docs/` as it is. When a harness doc changes what a
file must contain, change the generator too; `scripts/check-generator.mjs` (run by
`scripts/linting.sh` and by the Pages workflow before deploying) checks its output, so
keep the generators in the `AGW` object free of DOM access. Never put real signing values
(Apple identity, notary profile, Azure account or profile) in the repository; the docs
use placeholders and show how to look them up.

## Releasing

Releases are signed by hand on a Mac from the binaries CI builds for a `v*` tag, using
`scripts/release-download.sh`, `scripts/release-sign.sh` and
`scripts/release-upload.sh`. The steps and prerequisites are in `DEVELOPMENT.md`.
The download script takes only a release tag; the signing and upload scripts support
`--help` for their flags.
