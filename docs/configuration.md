# Configuration reference

## Where settings come from

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

## Windows installer

The `.msi` installs all three executables for every user of the machine, into
`C:\Program Files\Agent Gateway helper`, and adds that directory to the system `PATH`.
Installing needs administrator rights once; tokens and config stay per user. Installing
a newer `.msi` replaces the old version, and it is removed again under Settings > Apps
as "Agent Gateway helper". For unattended installs, e.g. from Intune:

```bat
msiexec /i agw-token-1.2.3-windows-amd64.msi /qn
msiexec /x agw-token-1.2.3-windows-amd64.msi /qn
```

An administrator can put an `agw-token.json` in the install directory to configure every
user of the machine. Its settings win over each user's own file (see
[Where settings come from](#where-settings-come-from)), and upgrades and uninstalls leave
it in place.

## Executable name

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

## Build-time default provider

Build a binary that falls back to a preset when nothing else picks a provider:

```sh
go build -ldflags "-X main.defaultProvider=google" -o agw-token ./cmd/agw-token
```

`scripts/build.sh` builds all three variants for a platform the way CI does.

`agw-token version` shows the compiled-in provider. An unknown name makes every run fail
with a configuration error.

CI builds one binary with no default and one for each preset, for example
`agw-token-darwin-arm64`, `agw-token-google-darwin-arm64` and `agw-token-entra-darwin-arm64`.

## Provider presets

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
value removes a preset parameter (`--extra-auth-params prompt=`). Parameters the
login sets itself (`state`, `nonce`, `code_challenge`, `code_challenge_method`,
`redirect_uri`, `client_id`, `response_type`, `scope`) are rejected; use `--scopes`
for scopes. In a profile, `extra-auth-params` can also be a JSON object, as in the
example above; its values may then contain commas, such as a JSON `claims` parameter. For example, to
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
> production, register your own client. [agent-gateway.md](agent-gateway.md) shows an
> agentgateway config that does these checks.

## Registering your own client

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

## Token lifetime

`agw-token` prints the stored token while it is valid for at least `--min-valid`
(default 5m). Otherwise it refreshes it with the refresh token, and opens the browser
again only if the refresh fails or returns a token that doesn't last that long. A token
valid for less than `--min-valid` is never printed: if even a new sign-in doesn't give
one, `agw-token` fails and says so.

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
