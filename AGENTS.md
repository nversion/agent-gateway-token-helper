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

Only the standard library and Go-team modules (`golang.org/x/...`). No go-oidc or JWT
libraries: ID token claims are decoded in `internal/idtoken` (see its package doc for why
signatures aren't verified).

## Tests

- `go test ./...` - unit tests, including in-process flows against `internal/oidctest`.
- `go test -tags integration ./test/...` - builds `agw-token` and runs it as a subprocess
  against the fake provider, playing the browser by following the `Open this URL:` line.
- `AGW_TEST_NATIVE_STORE=1 go test ./internal/store` - round-trips through the real
  Keychain / DPAPI / Secret Service. Opt-in because it touches the user's secret store.
