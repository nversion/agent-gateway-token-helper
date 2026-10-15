# agent-gateway-token-helper

`agw-token` prints an OpenID Connect **ID token** that AI harnesses (Claude Code and
others) can send to an [agentgateway](https://agentgateway.dev) instance. It works like
`gcloud auth print-identity-token` or `az account get-access-token`, but with any
standard OIDC provider:

- The first run opens the browser and signs in using the authorization code flow with
  PKCE and a loopback redirect (RFC 8252).
- Later runs print the stored token while it is valid for at least `--min-valid`
  (default 5m). Otherwise they refresh it with the refresh token, and open the browser
  again only if the refresh fails or returns a token that doesn't last that long. A
  token valid for less than `--min-valid` is never printed: if even a new sign-in
  doesn't give one, `agw-token` fails and says so.
- Tokens are kept in the platform's secret store (see [Token storage](docs/configuration.md#token-storage)).
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
default to those providers and need no flags. `SHA256SUMS` lists checksums for every
file:

```sh
shasum -a 256 --check --ignore-missing SHA256SUMS
```

**Windows:** the `.msi` installs the executables into
`C:\Program Files\Agent Gateway helper` and adds that directory to the system `PATH`.
Open a new terminal afterwards. Unattended installs and machine-wide configuration are
covered in [Configuration](docs/configuration.md#windows-installer).

**macOS and Linux:** unzip the archive and move the binaries onto your `PATH`:

```sh
sudo mv agw-token agw-token-google agw-token-entra /usr/local/bin/
```

To build from source or check a release against it, see [DEVELOPMENT.md](DEVELOPMENT.md).

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

Sign in once in a terminal to check it works. A browser window opens, and the token is
printed afterwards:

```sh
agw-token-google     # Google
agw-token-entra      # Entra ID, any work or school account
```

Then point your harness at the same binary. The examples send model traffic to a gateway
at `https://agw.example.com` and use `agw-token-google`.

**Claude Code**, in `~/.claude/settings.json`. Setup for MCP servers is in
[docs/claude-code.md](docs/claude-code.md).

```json
{
  "apiKeyHelper": "agw-token-google",
  "env": {
    "ANTHROPIC_BASE_URL": "https://agw.example.com",
    "CLAUDE_CODE_API_KEY_HELPER_TTL_MS": "240000"
  }
}
```

**Claude Desktop**, through managed settings. The `.reg` file and macOS configuration
profile are in [docs/claude-desktop.md](docs/claude-desktop.md).

| Key | Value |
|---|---|
| `inferenceProvider` | `gateway` |
| `inferenceGatewayBaseUrl` | `https://agw.example.com` |
| `inferenceCredentialKind` | `helper-script` |
| `inferenceCredentialHelper` | `/usr/local/bin/agw-token-google` |
| `inferenceCredentialHelperWindows` | `C:\Program Files\Agent Gateway helper\agw-token-google.exe` |
| `inferenceCredentialHelperTtlSec` | `240` |
| `inferenceCredentialHelperTimeoutSec` | `300` |

**Codex**, in `~/.codex/config.toml`. Details are in [docs/codex.md](docs/codex.md).

```toml
model_provider = "gateway"

[model_providers.gateway]
name = "Agent Gateway"
base_url = "https://agw.example.com/v1"
wire_api = "responses"

[model_providers.gateway.auth]
command = "/usr/local/bin/agw-token-google"
# Windows: command = 'C:\Program Files\Agent Gateway helper\agw-token-google.exe'
timeout_ms = 300000
refresh_interval_ms = 240000
```

The 4 minute caches stay under the 5 minutes of validity `agw-token` guarantees by
default, so a harness never holds an expired token. The 5 minute timeouts leave time for
a browser sign-in. Any other harness that runs a command for a credential works too, see
[docs/other-harnesses.md](docs/other-harnesses.md).

## Documentation

- [Claude Code](docs/claude-code.md): model traffic and MCP servers.
- [Claude Desktop](docs/claude-desktop.md): managed settings for Windows and macOS.
- [Codex](docs/codex.md): a custom model provider with command auth.
- [Other harnesses](docs/other-harnesses.md): any command-based credential.
- [Configuration](docs/configuration.md): profiles, presets, your own client, token storage and validation.
- [Development](DEVELOPMENT.md): building, testing, verifying and releasing.
