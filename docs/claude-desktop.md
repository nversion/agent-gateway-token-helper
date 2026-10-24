# Claude Desktop

Claude Desktop is configured through managed settings: a `.reg` file or Group Policy on
Windows, a configuration profile on macOS, a root-owned JSON file on Linux. The
[setup generator](https://nversion.dk/agent-gateway-token-helper/) produces these files
from a short form; this page explains what they contain. These keys send model traffic
through the gateway:

| Key | Value |
|---|---|
| `inferenceProvider` | `gateway` |
| `inferenceGatewayBaseUrl` | `https://agw.example.com` |
| `inferenceCredentialKind` | `helper-script` |
| `inferenceCredentialHelper` | full path to `agw-token-google` or `agw-token-entra` |
| `inferenceCredentialHelperWindows` | full Windows path (Windows only) |
| `inferenceCredentialHelperTtlSec` | `240` (4 minutes) |
| `inferenceCredentialHelperTimeoutSec` | `300` |

In a registry file or a configuration profile, every value is a string; Claude Desktop
also accepts `REG_DWORD` for the two integer keys. Only the Linux file takes JSON
numbers and arrays.

## Installing Claude Desktop on Windows

Install Claude Desktop via MSIX, as described in
[Deploy Claude Desktop for Windows](https://support.claude.com/en/articles/12622703-deploy-claude-desktop-for-windows).

Make sure the firewall allows `downloads.claude.ai`. Claude Desktop downloads the latest
version of Claude Code from there, as a private copy it uses for chats and cowork.

## Model traffic through the gateway

Claude Desktop on third-party inference (3P) can send model traffic to an
[LLM gateway](https://claude.com/docs/third-party/claude-desktop/gateway) and get its
bearer token from a
[credential helper](https://claude.com/docs/third-party/claude-desktop/credential-helper).
It runs the helper with no arguments by default. Point it at `agw-token-entra` or
`agw-token-google`, or [pass arguments](#passing-arguments) to `agw-token`.
The keys are described in the
[configuration reference](https://claude.com/docs/third-party/claude-desktop/configuration),
and [Deploy with MDM](https://claude.com/docs/third-party/claude-desktop/mdm) covers
rolling them out with Jamf, Intune or Group Policy.

The examples below cache the token for 4 minutes (`inferenceCredentialHelperTtlSec`
240), below `agw-token`'s default `min-valid` of 5 minutes, and give the helper 5 minutes
to finish a browser sign-in (`inferenceCredentialHelperTimeoutSec` 300). Sign in from
a terminal once with the same binary and configuration before starting Desktop.
To restrict the Entra preset to your tenant, put an `agw-token.json` next to the binary:

```json
{
  "profiles": {
    "default": { "tenant": "contoso.onmicrosoft.com" }
  }
}
```

**Windows:** the [MSI](configuration.md#windows-installer) puts `agw-token-entra.exe` in
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
"inferenceCredentialHelperTtlSec"=dword:000000f0
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
	<string>240</string>
	<key>inferenceCredentialHelperTimeoutSec</key>
	<string>300</string>
</dict>
</plist>
```

To test on a Mac without an MDM, either install the exported `.mobileconfig` by
double-clicking it and approving it under System Settings > General > Device Management,
or enter the same values in the app (Help > Troubleshooting > Enable Developer Mode, then
Developer > Configure Third-Party Inference, Apply Changes), which writes a local
configuration instead of a managed one. Copying the plist into
`/Library/Managed Preferences/` and running `sudo killall cfprefsd` also works for a quick
check, but the preferences daemon may overwrite or ignore a hand-placed file.

**Linux:** extract the tarball to a fixed location, for example `/usr/local/bin`. The
settings go in `/etc/claude-desktop/managed-settings.json` as plain JSON. Claude Desktop
only honors the file when it and its directory are owned by root, are not symlinks and
are not writable by anyone else:

```json
{
  "inferenceProvider": "gateway",
  "inferenceGatewayBaseUrl": "https://agw.example.com",
  "inferenceCredentialKind": "helper-script",
  "inferenceCredentialHelper": "/usr/local/bin/agw-token-entra",
  "inferenceCredentialHelperTtlSec": 240,
  "inferenceCredentialHelperTimeoutSec": 300
}
```

```sh
sudo install -d -m 755 -o root /etc/claude-desktop
sudo install -m 644 -o root managed-settings.json /etc/claude-desktop/
```

## Passing arguments

Use `inferenceCredentialHelperArgs` to select a provider or profile with the plain
`agw-token` executable. For example, these settings use the `work` profile from
[`agw-token.json`](configuration.md#where-settings-come-from):

```json
{
  "inferenceCredentialHelper": "/usr/local/bin/agw-token",
  "inferenceCredentialHelperWindows": "C:\\Program Files\\Agent Gateway helper\\agw-token.exe",
  "inferenceCredentialHelperArgs": ["--profile", "work"]
}
```

Merge these keys with the gateway settings above. The arguments can instead be
`["--provider", "entra", "--tenant", "contoso.onmicrosoft.com"]`.
In a Windows registry file or macOS configuration profile, encode the array as a JSON
string, as required by the [Desktop configuration reference](https://claude.com/docs/third-party/claude-desktop/configuration):

```reg
"inferenceCredentialHelperArgs"="[\"--profile\",\"work\"]"
```

```xml
<dict>
  <key>inferenceCredentialHelperArgs</key>
  <string>["--profile","work"]</string>
</dict>
```

The Linux file takes a JSON array: `"inferenceCredentialHelperArgs": ["--profile", "work"]`.

For an older Desktop release without this setting, use the provider binaries or a
[binary named for the profile](configuration.md#executable-name).

## Optional longer cache

To cache tokens for 30 minutes, set `inferenceCredentialHelperTtlSec` to `1800`
(`dword:00000708` in a `.reg` file) and set `"min-valid": "35m"` in the selected
helper profile. Keep `min-valid` above the Desktop TTL and below the lifetime your
provider issues; otherwise even a fresh sign-in may fail. The 4-minute examples above
work without this override.
