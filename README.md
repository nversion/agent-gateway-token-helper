# agent-gateway-token-helper

`agw-token` prints an OpenID Connect **ID token** that AI harnesses (Claude Code and
others) can send to an [agentgateway](https://agentgateway.dev) instance. It works like
`gcloud auth print-identity-token`, with support for standard OIDC providers:

- The first run opens the browser and signs in using the authorization code flow with
  PKCE and a loopback redirect (RFC 8252).
- Later runs reuse or refresh the stored token, and only open the browser again when
  that fails (see [Token lifetime](docs/configuration.md#token-lifetime)).
- Tokens are kept in the platform's secret store (see [Token storage](docs/configuration.md#token-storage)).
- On success, stdout contains only the token. Messages go to stderr. A failure exits
  non-zero.
- The `google` and `entra` presets sign in with the public clients of the gcloud SDK and
  the Azure CLI, so you don't need to register an app with your provider to get started.
- `agw-token-google` and `agw-token-entra` select those providers without flags.
  Profiles and environment variables supply any further settings.
- Release binaries are signed and notarized on macOS and signed with an MSI installer on
  Windows, so they can be deployed through MDM in enterprise environments (see
  [Install](#install)).

## Install

Download a prebuilt binary from the
[releases page](https://github.com/nversion/agent-gateway-token-helper/releases):

| OS | Archive | Signing |
|---|---|---|
| macOS | `agw-token-<version>-darwin-<arch>.zip` | Developer ID signed and notarized by Apple |
| Windows | `agw-token-<version>-windows-<arch>.msi` installer, or `.zip` | Authenticode signed with Azure Artifact Signing |
| Linux | `agw-token-<version>-linux-<arch>.tar.gz` | Not signed |

Choose `arm64` for Apple Silicon or an ARM64 Linux/Windows machine; choose `amd64`
for Intel or AMD x86-64. Replace `<version>` and `<arch>` below with the release number
(without `v`) and your architecture.

Each archive contains `agw-token`, `agw-token-google` and `agw-token-entra` (with `.exe`
on Windows). Download `SHA256SUMS` beside your archive and verify it before extraction
with the following command on macOS or Linux:

```sh
shasum -a 256 --check --ignore-missing SHA256SUMS
```

**Windows:** the `.msi` installs the executables into
`C:\Program Files\Agent Gateway helper` and adds that directory to the system `PATH`.
Open a new terminal afterwards. Unattended installs and machine-wide configuration are
covered in [Configuration](docs/configuration.md#windows-installer).

**macOS:** extract the ZIP:

```sh
unzip 'agw-token-<version>-darwin-<arch>.zip'
```

**Linux:** extract the tarball:

```sh
tar -xzf 'agw-token-<version>-linux-<arch>.tar.gz'
```

On either OS, install the extracted binaries onto your `PATH`:

```sh
sudo mkdir -p /usr/local/bin
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

Use `agw-token-google` or `agw-token-entra` for the same presets without flags.
`agw-token-entra` defaults to the `organizations` tenant. See
[Configuration](docs/configuration.md) to select a tenant, use your own client, or
configure a named profile.

The gateway must validate signatures and audiences **and** restrict the accepted
organization. The preset audiences are shared by all gcloud or Azure CLI users; see
[Provider presets](docs/configuration.md#provider-presets) and the
[minimal gateway example](docs/agent-gateway.md).

## Using it from a harness

Sign in once in a terminal with the same binary and configuration your harness will use.
Then follow its setup guide below. The default examples cache credentials for 4 minutes,
below the helper's default minimum validity of 5 minutes. Harnesses with short command
timeouts need browser sign-in completed beforehand.

For commands, output formats and logout behavior, see the
[command reference](docs/configuration.md#commands-and-output). Run `agw-token -h`
for CLI help.

## Documentation

The [setup generator](https://agw-token.nversion.dk/) fills in the
configuration files below for your gateway and provider, with download and copy buttons
and a note on where each file goes on Windows, macOS and Linux. The guides explain them:

- [Claude Code](docs/claude-code.md): model traffic and MCP servers.
- [Claude Desktop](docs/claude-desktop.md): managed settings for Windows, macOS and Linux.
- [Codex](docs/codex.md): a custom model provider with command auth.
- [Other harnesses](docs/other-harnesses.md): any command-based credential.
- [Agent gateway](docs/agent-gateway.md): a minimal authentication and model-routing example.
- [Gateway deployment](docs/agent-gateway-deployment.md): Cloud Run and Kubernetes, UI access and per-person budgets.
- [Configuration](docs/configuration.md): profiles, presets, your own client, token storage and validation.
- [Development](DEVELOPMENT.md): building, testing, verifying and releasing.
