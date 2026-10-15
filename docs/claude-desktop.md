# Claude Desktop

Claude Desktop is configured through managed settings: a `.reg` file or Group Policy on
Windows, a configuration profile on macOS. These keys send model traffic through the
gateway:

| Key | Value |
|---|---|
| `inferenceProvider` | `gateway` |
| `inferenceGatewayBaseUrl` | `https://agw.example.com` |
| `inferenceCredentialKind` | `helper-script` |
| `inferenceCredentialHelper` | full path to `agw-token-google` or `agw-token-entra` |
| `inferenceCredentialHelperWindows` | full Windows path (Windows only) |
| `inferenceCredentialHelperTtlSec` | how long Claude Desktop reuses a token, see below |
| `inferenceCredentialHelperTimeoutSec` | `300` |

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
