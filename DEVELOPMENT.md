# Development

This covers building, testing, verifying and releasing `agw-token`. For using it, see
the [README](README.md).

## Building

Build and install from source:

```sh
go install github.com/nversion/agent-gateway-token-helper/cmd/agw-token@latest
```

On macOS, build with cgo enabled (the default for native builds) so the Keychain can be
used. A `CGO_ENABLED=0` build falls back to a file store.

`scripts/build.sh <goos> <goarch>` builds the three release variants for a platform the
way CI does: `agw-token`, `agw-token-google` and `agw-token-entra`, into `build/`. It is
the only place build flags live. It pins the exact Go version from the `go` line in
`go.mod`, so bumping that line changes every release hash.

```sh
scripts/build.sh darwin arm64
scripts/build.sh windows amd64
```

Linux and Windows builds must stay byte-for-byte reproducible: no cgo, no VCS stamping,
nothing time- or path-dependent in ldflags. CI's `reproducible` job builds them twice and
diffs. macOS needs cgo and is not reproducible.

`scripts/build-msi.sh` builds the Windows Installer packages with `wixl` from msitools
(`brew install msitools`). The layout, install directory
`C:\Program Files\Agent Gateway helper` and the system `PATH` entry, is in
`packaging/windows/agw-token.wxs`. Never change its `UpgradeCode`: it is how a newer MSI
finds and replaces an installed one.

## Testing and linting

Install Go (the version in `go.mod`) and the `golangci-lint` version pinned in
`.github/workflows/build.yaml`. The following matches CI; rerun it when that pin changes:

```sh
curl -sSfL https://golangci-lint.run/install.sh | sh -s -- -b "$(go env GOPATH)/bin" v2.14.0
export PATH="$(go env GOPATH)/bin:$PATH"
```

For `scripts/linting.sh --fix`, also install `goimports` using the `golang.org/x/tools`
version in `go.mod`:

```sh
go install golang.org/x/tools/cmd/goimports@v0.50.0
```

If you set `GOBIN`, put that directory on `PATH` too. The script needs Bash and a native
C toolchain for race tests. `staticcheck` and `govulncheck` are pinned in `go.mod`'s
`tool` block and run through `go tool`; they do not need separate installs.

```sh
scripts/linting.sh                       # everything CI runs; --fix rewrites imports first
go test ./...                            # unit + in-process flow tests
go test -tags integration ./test/...     # runs the built binary against a fake provider
AGW_TEST_NATIVE_STORE=1 go test ./internal/store   # real Keychain/DPAPI/Secret Service
```

The native store test is opt-in because it touches your own secret store.
`scripts/linting.sh` also vets and lints with `GOOS=windows` and `GOOS=linux`, since
`internal/store`, `internal/lock` and `internal/browser` have per-OS files.

`cmd/fake-oidc` runs the test provider as a standalone server. It signs in
`alice@example.com` without showing a login page:

```sh
go run ./cmd/fake-oidc --addr 127.0.0.1:9999 &
AGW_HOME=$(mktemp -d) go run ./cmd/agw-token --issuer http://127.0.0.1:9999 --client-id agw-test-client
```

## CI

The Build workflow (`.github/workflows/build.yaml`) runs on pushes to `main`, pushes of
`v*` tags, pull requests targeting `main`, and manual dispatches:

| Job | What it does |
|---|---|
| `lint`, `test` | `scripts/linting.sh` and the unit and integration tests |
| `build` | `scripts/build.sh` for every platform; attests tag-build binaries on a public repository |
| `reproducible` | builds Linux and Windows twice and diffs the results |
| `msi` | builds the unsigned `.msi` packages |
| `msi-install` | installs and uninstalls one `.msi` on Windows |

The "Attest signed release" workflow (`.github/workflows/attest-signed.yaml`) is run by
`scripts/release-upload.sh`. It rebuilds Linux and Windows from the tag, checks the
uploaded assets and `SHA256SUMS-unsigned` against that rebuild, and attests every asset.

## Verifying a release against the source

Linux and Windows builds are reproducible: building a release tag yourself gives
byte-for-byte the binaries CI built, before signing. The release lists their checksums in
`SHA256SUMS-unsigned`. With Go installed (the exact version in `go.mod` is downloaded
automatically):

```sh
git clone https://github.com/nversion/agent-gateway-token-helper && cd agent-gateway-token-helper
git checkout v1.2.3
gh release download v1.2.3 --pattern SHA256SUMS-unsigned   # or from the releases page
scripts/build.sh linux amd64           # or windows amd64, windows arm64, linux arm64
(cd build && shasum -a 256 --check --ignore-missing ../SHA256SUMS-unsigned)
```

The Windows binaries you download carry an Authenticode signature, so their checksum
differs from your build. `verify-build` removes the signature and compares what is left,
for single binaries or whole release archives. Continue from the repository root:

```sh
gh release download v1.2.3 --pattern agw-token-1.2.3-windows-amd64.zip
go run ./cmd/verify-build SHA256SUMS-unsigned agw-token-1.2.3-windows-amd64.zip
```

The `.msi` installers are not reproducible themselves, since they hold timestamps and a
random product code, but the executables inside are the same signed files. Extract them
with `msiextract` from msitools (`brew install msitools`, `apt install msitools`), or
with `msiexec /a agw-token-1.2.3-windows-amd64.msi /qn TARGETDIR=C:\extract` on Windows,
and check those:

```sh
gh release download v1.2.3 --pattern agw-token-1.2.3-windows-amd64.msi
msiextract -C msi agw-token-1.2.3-windows-amd64.msi
find msi -type f -name '*.exe' -exec go run ./cmd/verify-build SHA256SUMS-unsigned {} +
```

macOS builds are not reproducible: they use cgo for the Keychain, and the C compiler and
SDK version end up in the binary. Check those through their signature and notarization,
and the build provenance attestation instead.

A full release from the public repository has GitHub attestations once the signing
upload's attestation workflow succeeds. The Build workflow attests the unsigned CI
binaries. The separate "Attest signed release" workflow attests uploaded assets after
rebuilding and comparing Linux and Windows; it does not reproduce the macOS build or
the manual signing operation. Partial uploads do not request attestations.

```sh
gh attestation verify agw-token-1.2.3-darwin-arm64.zip --repo nversion/agent-gateway-token-helper
codesign --verify --strict --verbose=2 agw-token     # after unzipping, macOS only
spctl --assess --type install --verbose agw-token-1.2.3-darwin-arm64.zip
```

## Releasing

Releases are signed by hand on a Mac, from the binaries CI builds for a `v*` tag. Set up
the [signing prerequisites](#signing-prerequisites) once, then:

**1. Tag the release.** The Build workflow builds the tag with `main.version` set to
`1.2.3` and attests each binary.

```sh
git tag v1.2.3 && git push origin v1.2.3
```

**2. Download the unsigned binaries.** This waits for the tag's Build run, downloads its
binaries into `dist/v1.2.3/unsigned` and checks their attestations.

```sh
scripts/release-download.sh v1.2.3
```

**3. Sign and package.** This codesigns and notarizes the macOS binaries, signs the
Windows ones with the vendored `tools/jsign-*.jar` through Azure Artifact Signing, builds
the `.msi` installers from the signed executables and signs those too. Everything lands
in `dist/v1.2.3/release`, with `SHA256SUMS` of the signed files and `SHA256SUMS-unsigned`
of the binaries as CI built them. It checks with `cmd/verify-build` that signing only
added a signature to the Windows binaries.

```sh
az login
scripts/release-sign.sh v1.2.3 \
    --apple-identity "Developer ID Application: <Organization> (<TEAMID>)" \
    --notary-profile <notary-profile> \
    --azure-endpoint https://<region>.codesigning.azure.net \
    --azure-account <signing-account> \
    --azure-profile <certificate-profile>
```

`--skip-macos` or `--skip-windows` leaves one OS unsigned, for example when you lack
the certificate for it.

**4. Upload.** This creates or updates a draft GitHub release and requests the
"Attest signed release" workflow. The upload script does not wait for that workflow.
Check the draft and wait for its attestation run to succeed before publishing:

```sh
scripts/release-upload.sh v1.2.3
gh run list --workflow attest-signed.yaml
# Use the run ID for this tag's upload from the list above.
gh run watch RUN_ID --exit-status
scripts/release-upload.sh v1.2.3 --publish
```

The publish command uploads the assets and requests attestation again; check that run
too before announcing the release.

It refuses unsigned builds unless you pass `--allow-unsigned`. `--only windows` uploads
one platform, and `--force` replaces assets on an already published release.
`release-sign.sh --help` and `release-upload.sh --help` list their flags.
`release-download.sh` takes only the release tag and does not support `--help`.

## Signing prerequisites

Common to both platforms:

```sh
gh auth login                  # the release scripts use gh
brew install msitools          # wixl and msiextract for the .msi packages
```

### macOS: Developer ID and notarization

You need the **Developer ID Application** certificate and its private key in the login
keychain. List the identities codesign can use, and pass the full quoted name as
`--apple-identity`:

```sh
security find-identity -v -p codesigning
#  1) 0123ABCD... "Developer ID Application: <Organization> (<TEAMID>)"
```

If the identity is missing or listed as invalid, import Apple's "Developer ID - G2"
intermediate certificate from
[Apple's certificate authority page](https://www.apple.com/certificateauthority/).

Notarization uses a `notarytool` keychain profile. Create it once with an
[app-specific password](https://support.apple.com/en-us/102654) for your Apple ID; the
name you choose is `--notary-profile`:

```sh
xcrun notarytool store-credentials <notary-profile> --apple-id <apple-id> --team-id <TEAMID>
```

`<TEAMID>` is the 10-character code in parentheses in the identity name. To check that an
existing profile works:

```sh
xcrun notarytool history --keychain-profile <notary-profile>
```

`notarytool` has no command to list saved profiles. If you don't know the name, store
the credentials again under a new one.

### Windows: Azure Artifact Signing

Signing runs the vendored `tools/jsign-*.jar`, so you need a Java runtime
(`brew install openjdk` if `java -version` fails) and the Azure CLI (`brew install
azure-cli`). Sign in as a user with the **Certificate Profile Signer** role on the
signing account:

```sh
az login
az account show --query '{subscription:name, user:user.name}'
```

Find the three flag values with the Azure CLI. Its extension still uses the product's
old name, `trustedsigning`, and installs itself on first use.

```sh
# --azure-account: the account name, plus its resource group for the commands below
az trustedsigning list --query '[].{account:name, group:resourceGroup, location:location}' -o table

# --azure-endpoint: the account URI without its trailing slash,
# e.g. https://neu.codesigning.azure.net for an account in North Europe
az trustedsigning show -n <signing-account> -g <resource-group> --query accountUri -o tsv

# --azure-profile: the certificate profile name
az trustedsigning certificate-profile list --account-name <signing-account> -g <resource-group> \
    --query '[].{profile:name, status:status}' -o table
```

If the subscription holding the account isn't your default, add `--subscription <name>`
or run `az account set --subscription <name>` first. To check you have the signing role:

```sh
az role assignment list --assignee "$(az account show --query user.name -o tsv)" --all \
    --query "[?contains(roleDefinitionName, 'Certificate Profile Signer')].scope" -o tsv
```

`release-sign.sh` gets its access token with
`az account get-access-token --resource https://codesigning.azure.net`, so if that
command fails, run `az login` again.
