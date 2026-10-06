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
| macOS | Login Keychain, generic password service `agw-token`, via Security.framework (cgo). An unsigned rebuilt binary triggers one "allow" prompt. |
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
