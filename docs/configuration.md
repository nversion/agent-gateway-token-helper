# Configuration reference

## Commands and output

Use `agw-token [command] [flags]`. Put the command before its flags and use the same
provider, tenant and profile settings for token, login, status and logout commands:

```sh
agw-token login --provider entra --tenant contoso.onmicrosoft.com
agw-token status --provider entra --tenant contoso.onmicrosoft.com
agw-token logout --provider entra --tenant contoso.onmicrosoft.com
```

| Command | Behavior |
|---|---|
| `token` (default) | Print an ID token, using stored tokens or signing in as needed. |
| `login` | Sign in again, ignoring stored tokens, and print the new ID token. |
| `logout` | Delete the tokens for the resolved configuration from the selected store. It does not revoke provider tokens or sign the browser out. |
| `status` | Inspect stored credentials without refreshing or signing in. Show the user, expiry, store and config files; never print a token. |
| `version` | Print the version and any compiled-in default provider. |
| `help`, `-h`, `--help` | Print CLI help. |

`status` exits 0 when a stored entry exists, even when its ID token has expired. Its
JSON output reports `logged_in`, `expired` and `has_refresh_token`; `expired` and the
refresh-token field are present only when an entry exists. An expired entry may still
be refreshable, and a successful status check does not test provider or gateway access.
No stored entry returns exit code 1. Other operational failures also return 1;
invalid commands, flags or configuration return 2.

`--output` controls token and login output:

| Value | Output |
|---|---|
| `raw` (default) | The ID token alone, followed by a newline. |
| `json` | An object with `id_token`, `expires_at` (UTC RFC 3339), `issuer`, `subject` and `email`. |
| `headers` | `{"Authorization":"Bearer …"}`, for MCP header helpers. |
| `decoded` | Pretty-printed JSON with `header` and `payload`. The signature is not verified. |

For `status`, `--output json` selects JSON; the other output values produce a readable
summary. Diagnostics go to stderr. See [Shared tokens and account selection](#shared-tokens-and-account-selection)
for the scope of login and logout.

## Flags and environment variables

These are the defaults for the plain binary with the `generic` provider and no
configuration overrides. [Provider presets](#provider-presets) supply different
defaults where listed there. Every row except `profile` is also a profile key using
the flag name without `--`.

| Flag | Environment variable | Default | Accepted value or purpose |
|---|---|---|---|
| `--profile` | `AGW_PROFILE` | Executable-selected profile, otherwise `default` | Name of a profile; an explicitly selected profile must exist. |
| `--provider` | `AGW_PROVIDER` | `generic` | `generic`, `google`, `entra`; executable or build defaults may select a preset. |
| `--issuer` | `AGW_ISSUER` | Required | HTTPS OIDC issuer URL; may contain `{tenant}`. HTTP is allowed only on loopback for tests. |
| `--tenant` | `AGW_TENANT` | Empty | Tenant ID or domain substituted into `{tenant}`; Entra defaults to `organizations`. |
| `--client-id` | `AGW_CLIENT_ID` | Required | Public OAuth client ID. |
| `--client-secret` | `AGW_CLIENT_SECRET` | Empty | Client secret for native clients whose provider requires it. |
| `--scopes` | `AGW_SCOPES` | `openid email profile offline_access` | Space- or comma-separated scopes; must include `openid`. JSON profiles also accept an array of strings. |
| `--extra-auth-params` | `AGW_EXTRA_AUTH_PARAMS` | Empty | `key=value,key=value`; a JSON profile also accepts an object of string values. Merged with preset parameters. |
| `--no-account-picker` | `AGW_NO_ACCOUNT_PICKER` | `false` | Boolean; remove `select_account` from the login prompt. |
| `--min-valid` | `AGW_MIN_VALID` | `5m` | Nonnegative Go duration; only print tokens with more than this much life remaining. |
| `--redirect-host` | `AGW_REDIRECT_HOST` | `localhost` | `localhost`, `127.0.0.1`, `[::1]`. |
| `--redirect-port` | `AGW_REDIRECT_PORT` | `0` | `0` for a free port, `1`–`65535` for a fixed port, or `N+` to try up to 100 consecutive ports. |
| `--callback-path` | `AGW_CALLBACK_PATH` | `/callback` | Plain absolute URL path, or empty for no path; no query, fragment, escaping or dot segments. |
| `--no-browser` | `AGW_NO_BROWSER` | `false` | Boolean; print the URL and wait for a manual browser sign-in. This does not disable interactive login. |
| `--login-timeout` | `AGW_LOGIN_TIMEOUT` | `5m` | Nonnegative Go duration limiting browser sign-in. |
| `--store` | `AGW_STORE` | `auto` | `auto` for the platform backend, `file` for a file store. |
| `--output` | `AGW_OUTPUT` | `raw` | `raw`, `json`, `headers`, `decoded`; see [Commands and output](#commands-and-output). |

Boolean flags can be written as `--no-browser` or `--no-browser=false`. In environment
variables use `true` or `false`; JSON profiles can use JSON booleans. Durations use
units such as `s`, `m` and `h`, for example `30s` or `1h30m`.

`AGW_HOME` is an additional environment-only setting for the config, cache and file-store
directories; it has no flag or profile key. `-h` and `--help` have no environment equivalent.

## Where settings come from

Settings can come from several places. The first one that sets a value wins:

1. command line flags, e.g. `--client-id`
2. environment variables: `AGW_` plus the flag name, e.g. `AGW_CLIENT_ID`, `AGW_PROFILE`
3. a profile in `agw-token.json` next to the executable
4. a profile in `agw-token.json` in the user config directory
5. the executable name: `agw-token-<name>` picks a profile or provider
6. the provider compiled in at build time
7. the provider preset
8. the generic defaults in the table above

The profile is `--profile`, else `AGW_PROFILE`, else the executable name if a profile has
that name, else `default`. When both `agw-token.json` files exist they are merged one
setting at a time, and the file next to the executable wins. A named profile only has to
exist in one of them. `agw-token status` with the same provider/profile settings lists
the config files it read.

Named profiles do not inherit settings from `default`. Only the selected profile is
merged across the two files. In particular, `extra-auth-params` is selected as one
setting from the highest-precedence source, then merged with the provider preset;
its object members are not merged between config files or with the environment.
Empty environment variables are ignored. To override a lower-precedence string with
an empty value, use an explicit flag such as `--client-secret=` or a JSON empty string.

The user config directory is `$XDG_CONFIG_HOME/agw-token/` on Linux (default
`~/.config/agw-token/`), `~/Library/Application Support/agw-token/` on macOS and
`%APPDATA%\agw-token\` on Windows. The executable's directory is resolved through
symlinks, so the file goes next to the real binary. Example:

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

## macOS installer

The `.pkg` installs all three executables for every user of the machine, into
`/usr/local/bin`, and `LICENSE` and `README.md` into `/usr/local/share/agw-token`.
Installing needs administrator rights once; tokens and config stay per user. Installing a
newer `.pkg` replaces the old version. The `arm64` package installs on Apple Silicon only;
the `amd64` one also installs there and runs through Rosetta. For unattended installs,
e.g. from an MDM:

```sh
sudo installer -pkg agw-token-1.2.3-darwin-arm64.pkg -target /
```

macOS keeps a receipt but has no uninstaller. To remove the files and the receipt:

```sh
pkgutil --only-files --files dk.nversion.agw-token | sed 's|^|/|' | sudo xargs rm -f
sudo pkgutil --forget dk.nversion.agw-token
```

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

`agw-token version` shows the compiled-in provider. An unknown name makes commands
that load configuration fail; version and help still work.

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
> it must also check the user's organization: `hd` for Google Workspace, `tid` (and
> `oid` or roles) for Entra. Tokens from the Azure CLI client have no `groups` claim.
> Google and Microsoft can also restrict their first-party clients at any time. For
> production, register your own client. [agent-gateway.md](agent-gateway.md) shows an
> agentgateway config that does these checks.

For Google Workspace, require `email_verified` and an allowed `hd` claim. An email
suffix alone does not establish Workspace membership, even when `email_verified` is
true; see [Google's ID-token validation guidance](https://developers.google.com/identity/gsi/web/guides/verify-google-id-token).

## Registering your own client

Register a **public** (native/desktop) client that uses the authorization code flow with
PKCE and a loopback redirect URI:

- **Entra ID:** add the platform "Mobile and desktop applications" with redirect URI
  `http://localhost`. Entra ignores the port for loopback addresses. Then run with
  `--provider entra --client-id <app id> --tenant <tenant>`.
- **Google:** create an OAuth client of type "Desktop app" and use both its client ID
  and its (non-secret, for installed applications) client secret:

  ```sh
  agw-token --provider google \
    --client-id YOUR_CLIENT_ID.apps.googleusercontent.com \
    --client-secret YOUR_DESKTOP_CLIENT_SECRET
  ```

  Keep `--provider google` so the Google scopes and offline-access parameters apply.
  Add this client ID to the gateway's accepted Google audiences too.
- **Keycloak, Okta, Auth0, Dex, …:** use a public client with PKCE (S256) and redirect
  URI `http://localhost:8400/callback` with `--redirect-port 8400`, or the provider's
  supported loopback-port matching syntax if it accepts variable ports. Include
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

## Shared tokens and account selection

Token storage is keyed by the resolved issuer URL, client ID, scopes (order-independent)
and extra authorization parameters, excluding `prompt`. The profile name is not part
of that key. Equivalent profiles share credentials in the same store, and changing
`min-valid`, output format or account-picker behavior does not create another account.

`login` replaces the entry for that key after a successful sign-in. `logout` deletes
that entry, affecting every profile that shares it; it does not clear all profiles or
all stores. Concurrent token requests using the same key and state directory share a
lock, so they can reuse the first successful sign-in.

Use separate issuer/client settings when the accounts require different configurations.
If you need independent accounts with identical provider settings, use separate
`AGW_HOME` directories **and** `--store file`. Changing `AGW_HOME` alone does not isolate
entries in the macOS Keychain or Linux Secret Service. File storage has the protections
described above rather than native-store encryption.

## How tokens are validated

The ID token always comes straight from the provider's token endpoint over TLS. As OIDC
Core §3.1.3.7 allows in that case, `agw-token` checks the claims but not the signature:

- `iss` (Entra's `{tenantid}` template is resolved from `tid`)
- `aud` contains the client ID, and `azp`, if present, is the client ID
- `exp` and `iat`
- `nonce` after sign-in
- unchanged `sub` and resolved `iss` after a refresh

The gateway verifies the signature. `http://` issuers are accepted only for loopback
hosts, which is meant for local testing.
