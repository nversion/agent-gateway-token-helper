# agent-gateway-token-helper

`agw-token` prints an OpenID Connect **ID token** that AI harnesses (Claude Code and
others) can send to an [agentgateway](https://agentgateway.dev) instance. It works like
`gcloud auth print-identity-token` or `az account get-access-token`, but with any
standard OIDC provider:

- The first run opens the browser and signs in using the authorization code flow with
  PKCE and a loopback redirect (RFC 8252).
- Later runs print the stored token while it is valid for at least `--min-valid`
  (default 5m). Otherwise they refresh it with the refresh token, and open the browser
  again only if the refresh fails.
- Tokens are kept in the platform's secret store (see [Token storage](#token-storage)).
- On success, stdout contains only the token. Messages go to stderr. A failure exits
  non-zero.

## Install

Download a prebuilt binary from the
[releases page](https://github.com/nversion/agent-gateway-token-helper/releases):

| OS | Archive | Signing |
|---|---|---|
| macOS | `agw-token-<version>-darwin-<arch>.zip` | Developer ID signed and notarized by Apple |
| Windows | `agw-token-<version>-windows-<arch>.msi` installer, or `.zip` | Authenticode signed with Azure Artifact Signing |
| Linux | `agw-token-<version>-linux-<arch>.tar.gz` | Not signed |

Each archive contains `agw-token` plus `agw-token-google` and `agw-token-entra`, which
default to those providers (see [Executable name](#executable-name)). The binaries are
also attached individually, and `SHA256SUMS` lists checksums for every file:

```sh
shasum -a 256 --check --ignore-missing SHA256SUMS
```

### Windows installer

The `.msi` installs all three executables for every user of the machine, into
`C:\Program Files\Agent Gateway helper`, and adds that directory to the system `PATH`.
Installing needs administrator rights once; tokens and config stay per user. Open a new
terminal afterwards to pick up the `PATH` change. Installing a newer `.msi` replaces the
old version, and it is removed again under Settings > Apps as "Agent Gateway helper".
For unattended installs, e.g. from Intune:

```bat
msiexec /i agw-token-1.2.3-windows-amd64.msi /qn
msiexec /x agw-token-1.2.3-windows-amd64.msi /qn
```

An administrator can put an `agw-token.json` in the install directory to configure every
user of the machine. Its settings win over each user's own file (see
[Configuration](#configuration)), and upgrades and uninstalls leave it in place.

### Verifying a release against the source

Linux and Windows builds are reproducible: building a release tag yourself gives
byte-for-byte the binaries CI built, before signing. The release lists their checksums in
`SHA256SUMS-unsigned`. With Go installed (the exact version in `go.mod` is downloaded
automatically):

```sh
git clone https://github.com/nversion/agent-gateway-token-helper && cd agent-gateway-token-helper
git checkout v1.2.3
gh release download v1.2.3 --pattern SHA256SUMS-unsigned   # or from the releases page
scripts/build.sh linux amd64           # or windows amd64, windows arm64, linux arm64
cd build && shasum -a 256 --check --ignore-missing ../SHA256SUMS-unsigned
```

The Windows binaries you download carry an Authenticode signature, so their checksum
differs from your build. `verify-build` removes the signature and compares what is left,
for single binaries or whole release archives:

```sh
go run ./cmd/verify-build SHA256SUMS-unsigned agw-token-1.2.3-windows-amd64.zip
```

The `.msi` installers are not reproducible themselves, since they hold timestamps and a
random product code, but the executables inside are the same signed files. Extract them
with `msiextract` from msitools (`brew install msitools`, `apt install msitools`), or
with `msiexec /a agw-token-1.2.3-windows-amd64.msi /qn TARGETDIR=C:\extract` on Windows,
and check those:

```sh
msiextract -C msi agw-token-1.2.3-windows-amd64.msi
go run ./cmd/verify-build SHA256SUMS-unsigned msi/*/*.exe
```

macOS builds are not reproducible: they use cgo for the Keychain, and the C compiler and
SDK version end up in the binary. Check those through their signature and notarization,
and the build provenance attestation instead.

Every release file has a GitHub build provenance attestation. It ties the file to this
repository, the tag's commit and the workflow that built it:

```sh
gh attestation verify agw-token-1.2.3-darwin-arm64.zip --repo nversion/agent-gateway-token-helper
codesign --verify --strict --verbose=2 agw-token     # after unzipping, macOS only
spctl --assess --type install --verbose agw-token-1.2.3-darwin-arm64.zip
```

Or build from source:

```sh
go install github.com/nversion/agent-gateway-token-helper/cmd/agw-token@latest
```

On macOS, build with cgo enabled (the default for native builds) so the Keychain can be
used. A `CGO_ENABLED=0` build falls back to a file store.

## Quick start

```sh
# Microsoft Entra ID, using the Azure CLI's public client
agw-token --provider entra --tenant contoso.onmicrosoft.com

# Google, using the gcloud SDK's public client
agw-token --provider google

# Any OIDC provider, using your own public client
agw-token --issuer https://idp.example.com/realms/main --client-id agent-gateway
```

| Command | What it does |
|---|---|
| `agw-token [token]` | Print the ID token (default) |
| `agw-token login` | Sign in again, ignoring stored tokens |
| `agw-token logout` | Delete the stored tokens |
| `agw-token status` | Show the signed-in user, expiry and store. Never prints the token. Exits 1 when not signed in |
| `agw-token version` | Print the version |

`--output raw|json|headers|decoded` chooses the token format:

- `raw`: the token alone.
- `json`: an object with token, expiry, subject and issuer.
- `headers`: `{"Authorization":"Bearer …"}`.
- `decoded`: the token's header and payload as pretty-printed JSON, for debugging. The signature is not verified.

Run `agw-token -h` to see all flags.

## Using it from a harness

### Claude Code: MCP servers behind the gateway

`headersHelper` runs a command that prints a JSON object of HTTP headers:

```json
{
  "mcpServers": {
    "gateway": {
      "type": "http",
      "url": "https://agw.example.com/mcp",
      "headersHelper": "agw-token --profile work --output headers"
    }
  }
}
```

### Claude Code: model traffic through the gateway

Set `ANTHROPIC_BASE_URL` to the gateway, and set `apiKeyHelper` in `settings.json`:

```json
{
  "apiKeyHelper": "agw-token --profile work",
  "env": {
    "ANTHROPIC_BASE_URL": "https://agw.example.com",
    "CLAUDE_CODE_API_KEY_HELPER_TTL_MS": "300000"
  }
}
```

### Claude Desktop: model traffic through the gateway

Claude Desktop on third-party inference (3P) can send model traffic to an
[LLM gateway](https://claude.com/docs/third-party/claude-desktop/gateway) and get its
bearer token from a
[credential helper](https://claude.com/docs/third-party/claude-desktop/credential-helper).
It runs the helper with no arguments, so point it at a binary that needs no flags, such
as `agw-token-entra` or `agw-token-google`. The keys are described in the
[configuration reference](https://claude.com/docs/third-party/claude-desktop/configuration),
and [Deploy with MDM](https://claude.com/docs/third-party/claude-desktop/mdm) covers
rolling them out with Jamf, Intune or Group Policy.

The examples below cache the token for 30 minutes (`inferenceCredentialHelperTtlSec`
1800) and give the helper 5 minutes to finish a browser sign-in
(`inferenceCredentialHelperTimeoutSec` 300). Claude Desktop reuses the printed token for
the whole TTL, so set `min-valid` above it in an `agw-token.json` next to the binary.
Otherwise `agw-token` may print a token with only minutes left. With Entra, that file is
also the place for your tenant:

```json
{
  "profiles": {
    "default": { "tenant": "contoso.onmicrosoft.com", "min-valid": "35m" }
  }
}
```

**Windows:** the [MSI](#windows-installer) puts `agw-token-entra.exe` in
`C:\Program Files\Agent Gateway helper`. Save this as `claude-gateway.reg` and import it
as the user who runs Claude Desktop, or set the same values under
`HKLM\SOFTWARE\Policies\Claude` for every user of the machine (the app then ignores
`HKCU`):

```reg
Windows Registry Editor Version 5.00

[HKEY_CURRENT_USER\SOFTWARE\Policies\Claude]
"inferenceProvider"="gateway"
"inferenceGatewayBaseUrl"="https://agw.example.com"
"inferenceCredentialKind"="helper-script"
"inferenceCredentialHelper"="C:\\Program Files\\Agent Gateway helper\\agw-token-entra.exe"
"inferenceCredentialHelperWindows"="C:\\Program Files\\Agent Gateway helper\\agw-token-entra.exe"
"inferenceCredentialHelperTtlSec"=dword:00000708
"inferenceCredentialHelperTimeoutSec"=dword:0000012c
```

**macOS:** unzip the release to a fixed location, for example
`/usr/local/bin/agw-token-entra`. The settings go in the `com.anthropic.claudefordesktop`
preference domain, delivered by a configuration profile. The in-app configuration window
can export one, or add these keys to the payload of your MDM's custom settings:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>inferenceProvider</key>
	<string>gateway</string>
	<key>inferenceGatewayBaseUrl</key>
	<string>https://agw.example.com</string>
	<key>inferenceCredentialKind</key>
	<string>helper-script</string>
	<key>inferenceCredentialHelper</key>
	<string>/usr/local/bin/agw-token-entra</string>
	<key>inferenceCredentialHelperTtlSec</key>
	<integer>1800</integer>
	<key>inferenceCredentialHelperTimeoutSec</key>
	<integer>300</integer>
</dict>
</plist>
```

### Other harnesses

Any harness that can run a command to get a credential can use `agw-token` (raw output)
or `agw-token --output headers`. Calls are cheap when the stored token is still valid,
taking a few milliseconds with no network access. Concurrent calls are serialized per
profile, so parallel tool calls open at most one browser window.

Some harnesses, such as Claude Desktop, run the command without arguments. Configure
those without flags in any of these ways:
[an `agw-token.json` next to the binary](#configuration), a
[binary named after the profile or provider](#executable-name) such as
`agw-token-google`, or a [build with a default provider](#build-time-default-provider).

## Configuration

Settings can come from several places. The first one that sets a value wins:

1. command line flags, e.g. `--client-id`
2. environment variables: `AGW_` plus the flag name, e.g. `AGW_CLIENT_ID`, `AGW_PROFILE`
3. a profile in `agw-token.json` next to the executable
4. a profile in `agw-token.json` in the user config directory
5. the executable name: `agw-token-<name>` picks a profile or provider
6. the provider compiled in at build time
7. the provider preset

The profile is `--profile`, else `AGW_PROFILE`, else the executable name if a profile has
that name, else `default`. When both `agw-token.json` files exist they are merged one
setting at a time, and the file next to the executable wins. A named profile only has to
exist in one of them. `agw-token status` lists the config files it read.

The user config directory is `~/.config/agw-token/` on Linux,
`~/Library/Application Support/agw-token/` on macOS and `%APPDATA%\agw-token\` on
Windows. The executable's directory is resolved through symlinks, so the file goes next
to the real binary. Example:

```json
{
  "profiles": {
    "default": { "provider": "entra", "tenant": "contoso.onmicrosoft.com" },
    "work": {
      "issuer": "https://idp.example.com/realms/main",
      "client-id": "agent-gateway",
      "scopes": ["openid", "email", "offline_access"],
      "extra-auth-params": { "audience": "agent-gateway" }
    }
  }
}
```

Set `AGW_HOME` to keep config, cache and file-store tokens in a single directory. It
replaces the user config directory. The file next to the executable is still read.

### Executable name

Renaming or copying the binary to `agw-token-<name>` (`agw-token-<name>.exe` on Windows)
works like `--profile <name>` if a profile called `<name>` exists. Otherwise it works like
`--provider <name>` if `<name>` is a preset. On Unix a symlink also works, since the name
comes from how the binary was invoked. Release artifact names like
`agw-token-google-darwin-arm64` also work: if the full suffix matches nothing, the part
before the first dash is tried. A name that matches nothing, such as
`agw-token-linux-amd64`, is ignored.

```sh
cp agw-token agw-token-google      # Google preset, no flags needed
cp agw-token agw-token-work        # the "work" profile from agw-token.json
```

A `provider` set in the selected profile, including the `default` profile, wins over the
name.

### Build-time default provider

Build a binary that falls back to a preset when nothing else picks a provider:

```sh
go build -ldflags "-X main.defaultProvider=google" -o agw-token ./cmd/agw-token
```

`scripts/build.sh` builds all three variants for a platform the way CI does.

`agw-token version` shows the compiled-in provider. An unknown name makes every run fail
with a configuration error.

CI builds one binary with no default and one for each preset, for example
`agw-token-darwin-arm64`, `agw-token-google-darwin-arm64` and `agw-token-entra-darwin-arm64`.

### Provider presets

| | `google` | `entra` |
|---|---|---|
| Client | gcloud SDK (`32555940559.apps.googleusercontent.com`) | Azure CLI (`04b07795-8ddb-461a-bbee-02f9e1bf7b46`) |
| Issuer | `https://accounts.google.com` | `https://login.microsoftonline.com/{tenant}/v2.0`, `--tenant` default `organizations` |
| Scopes | `openid email`, plus `access_type=offline`, `prompt=select_account consent` | `openid profile email offline_access`, plus `prompt=select_account` (same as `az login`) |
| `--no-account-picker` | sends only `prompt=consent`: reuses the single signed-in account, Google still asks when several are signed in | sends no `prompt`: reuses the browser's signed-in account |
| Redirect | `http://localhost:<8085…8184>/` (same as gcloud) | `http://localhost:<random>` (same as MSAL) |
| ID token `aud` | the gcloud client ID | the Azure CLI client ID |

Any preset value can be overridden. `extra-auth-params` is the exception: your
parameters are added to the preset's, with yours winning on conflicts, and an empty
value removes a preset parameter (`--extra-auth-params prompt=`). For example, to
limit Google's account picker to your Workspace domain:

```sh
agw-token --provider google --extra-auth-params hd=example.com
```

The `hd` parameter only filters the login screen. The gateway still has to check the
`hd` claim.

> **Restrict more than the audience on the gateway.** Every gcloud or Azure CLI user in
> the world can get a token with these audiences. When the gateway accepts preset tokens,
> it must also check the user's organization: `hd` or `email` for Google, `tid` (and
> `oid` or roles) for Entra. Tokens from the Azure CLI client have no `groups` claim.
> Google and Microsoft can also restrict their first-party clients at any time. For
> production, register your own client.

### Registering your own client

Register a **public** (native/desktop) client that uses the authorization code flow with
PKCE and a loopback redirect URI:

- **Entra ID:** add the platform "Mobile and desktop applications" with redirect URI
  `http://localhost`. Entra ignores the port for loopback addresses. Then run with
  `--provider entra --client-id <app id> --tenant <tenant>`.
- **Google:** create an OAuth client of type "Desktop app". Google requires its
  (non-secret) client secret: pass `--client-secret`.
- **Keycloak, Okta, Auth0, Dex, …:** use a public client with PKCE (S256) and redirect
  URI `http://localhost:*/callback` (or a fixed `--redirect-port`). Include
  `offline_access` in the scopes if the provider needs it to issue refresh tokens.

Providers that require an exact redirect URI need a fixed port: `--redirect-port 8400`.
Use `--redirect-port 8400+` to also try the next 99 ports.

## Token storage

| OS | `--store auto` (default) |
|---|---|
| macOS | Login Keychain, generic password service `agw-token`, via Security.framework (cgo). Release binaries are signed, so updates keep access. An unsigned or rebuilt binary triggers one "allow" prompt. |
| Windows | Files in `%LOCALAPPDATA%\agw-token\tokens`, encrypted with DPAPI for the current user. |
| Linux | Secret Service (GNOME Keyring, KWallet, KeePassXC) through `secret-tool`, with secrets passed on stdin. Falls back to a 0600 file in `~/.config/agw-token/tokens` when `secret-tool` is missing or no session bus is reachable (e.g. headless or SSH). |

`--store file` always uses the 0600 file. `agw-token status` shows which store is in use.

## How tokens are validated

The ID token always comes straight from the provider's token endpoint over TLS. As OIDC
Core §3.1.3.7 allows in that case, `agw-token` checks the claims but not the signature:

- `iss` (Entra's `{tenantid}` template is resolved from `tid`)
- `aud` contains the client ID, and `azp`, if present, is the client ID
- `exp` and `iat`
- `nonce` after sign-in
- unchanged `sub` after a refresh

The gateway verifies the signature. `http://` issuers are accepted only for loopback
hosts, which is meant for local testing.

## Development

```sh
scripts/linting.sh                       # everything CI runs
go test ./...                            # unit + in-process flow tests
go test -tags integration ./test/...     # runs the built binary against a fake provider
AGW_TEST_NATIVE_STORE=1 go test ./internal/store   # real Keychain/DPAPI/Secret Service
```

`cmd/fake-oidc` runs the test provider as a standalone server. It signs in
`alice@example.com` without showing a login page:

```sh
go run ./cmd/fake-oidc --addr 127.0.0.1:9999 &
AGW_HOME=$(mktemp -d) go run ./cmd/agw-token --issuer http://127.0.0.1:9999 --client-id agw-test-client
```
