# Deploying with an MDM

This page is for the administrator who rolls the helper and Claude Desktop out to a
fleet with Intune, Jamf, Group Policy or another device manager. It covers what to
deploy, how each MDM takes the files, and the network rule that most often breaks Claude
Desktop afterwards. The per-harness guides explain what the files contain; the
[setup generator](https://agw-token.nversion.dk/) produces them.

Deploy in this order, so users open Claude Desktop for the first time and land in the
gateway configuration:

1. The helper installer: the `.msi` or `.pkg` from the
   [releases page](https://github.com/nversion/agent-gateway-token-helper/releases),
   plus an optional machine-wide `agw-token.json`.
2. The Claude Desktop managed settings: the `.reg`, `.mobileconfig` or `.plist` from
   the [Claude Desktop guide](claude-desktop.md).
3. The network allowances below.
4. Claude Desktop itself, following Anthropic's
   [Deploy with MDM](https://claude.com/docs/third-party/claude-desktop/mdm) page.

## Network: Claude Desktop downloads Claude Code

Claude Desktop does not ship with Claude Code. It downloads the Claude Code binary and
the Cowork workspace bundle from `downloads.claude.ai` at first start and after most
updates, and chats and tasks cannot start until it has. Allow the Claude Desktop
process, not only browsers, to reach that host. With the gateway provider the full set
of hosts is `downloads.claude.ai`, the gateway, and the identity provider the helper
signs in with (`login.microsoftonline.com` for Entra ID; `accounts.google.com` and
`oauth2.googleapis.com` for Google). Anthropic's
[Telemetry and egress](https://claude.com/docs/third-party/claude-desktop/telemetry#required-egress-paths)
page lists the hosts that other settings add.

### Microsoft Defender for Cloud Apps

Microsoft Defender for Cloud Apps (formerly Microsoft Cloud App Security) pushes every
app tagged **Unsanctioned** in Cloud Discovery to the devices as a block indicator, which
network protection enforces for all processes. The block covers every domain of the
app, so an **Unsanctioned** tag on Claude or Anthropic, set by hand or by an app
discovery policy, blocks `downloads.claude.ai` and with it Claude Desktop, even when the
model traffic goes to your own gateway. A Defender administrator lifts it in the
[Microsoft Defender portal](https://security.microsoft.com):

1. Under **Cloud apps > Cloud discovery > Discovered apps** (or **Cloud app catalog**),
   find the Claude entry and remove the **Unsanctioned** tag. Changes take up to three
   hours to reach devices. See Microsoft's
   [Govern discovered apps](https://learn.microsoft.com/en-us/defender-cloud-apps/mde-govern).
2. If an app discovery policy applied the tag, exclude the app from the policy or
   change its risk threshold, or the next policy run tags it again.
3. Under **Settings > Endpoints > Indicators > URLs/Domains**, remove any block entry for
   `downloads.claude.ai`, `claude.ai` or `anthropic.com`, or add `downloads.claude.ai`
   with the action **Allow**, which wins over warn and block indicators. Sanctioning an
   app creates no allow indicator by itself. See
   [Create indicators for IPs and URLs/domains](https://learn.microsoft.com/en-us/defender-endpoint/indicator-ip-domain).
4. Have the user restart Claude Desktop and start a new chat, which repeats the
   download.

#### Scoping the allowance

The allowance can be as narrow as one hostname for one set of devices, but not narrower:

- **Per hostname:** keep the **Unsanctioned** tag on Claude, so `claude.ai`,
  `api.anthropic.com` and the other Anthropic hosts stay blocked, and add the **Allow**
  indicator for `downloads.claude.ai` alone. Allow wins over the block the tag created,
  and covers only that host.
- **Per device group:** every indicator has a **Scope** that selects Defender for
  Endpoint device groups, and the **Unsanctioned** tag can be limited with a scoped
  profile under **Settings > Cloud apps > App tags > Scoped profiles**. Scope the tag
  through a scoped profile, not by editing the indicator it created, which Defender for
  Cloud Apps overwrites.
- **Per user:** not directly. Device groups are built from device properties (name,
  domain, OS, tags), never from the signed-in user. To follow a user group, set the
  Defender device tag (the `Group` string value under
  `HKLM\SOFTWARE\Policies\Microsoft\Windows Advanced Threat Protection\DeviceTagging`)
  with an Intune configuration profile assigned to that Entra group, build a device
  group on the tag, and scope the **Allow** indicator to it.

Where the organization cannot allow `downloads.claude.ai`, Anthropic publishes an
[offline installer](https://claude.com/docs/third-party/claude-desktop/installation#offline-installation)
with both components built in. Pair it with `disableAutoUpdates` and
`modelCatalogEnabled` set to `false` in the managed settings, and distribute each new
version yourself.

## Deploying the helper

The installers are signed, install for every user of the machine and replace an older
version in place; see the [Windows installer](configuration.md#windows-installer) and
[macOS installer](configuration.md#macos-installer) sections. Tokens stay per user in
the Keychain or DPAPI, so nothing on the device holds a shared credential.

### Windows

**Intune:** add the `.msi` as a line-of-business app under **Apps > All apps > Create >
Windows > Line-of-business app**. Intune reads the install context (per machine) and
version from the package and installs silently, so no command-line arguments are
needed. To update, open the app's properties and select the newer `.msi` under
**App information**; the package replaces the installed version. The same `.msi` also
works as a Win32 app (`.intunewin`), with `msiexec /i agw-token-<version>-windows-<arch>.msi /qn`
as the install command and `agw-token.exe` in `C:\Program Files\Agent Gateway helper`
as the detection rule. Microsoft's page:
[Add a Windows line-of-business app](https://learn.microsoft.com/en-us/intune/intune-service/apps/lob-apps-windows).

**Group Policy:** assign the `.msi` from a share under **Computer Configuration >
Policies > Software Settings > Software installation**. It installs at the next startup.

**Machine-wide settings:** an `agw-token.json` in the install directory configures every
user of the machine (see [Where settings come from](configuration.md#where-settings-come-from)).
Intune cannot place a file by itself: write it from a PowerShell script under **Devices >
Scripts and remediations**, run in the system context, or add it to a Win32 app's
install command. Group Policy Preferences can copy it under **Computer Configuration >
Preferences > Windows Settings > Files**.

The installer adds its directory to the system `PATH`, which processes started before
the install do not see until they restart. Claude Desktop uses the full path from its
settings and is not affected.

### macOS

**Jamf Pro:** upload the `.pkg` under **Settings > Computer Management > Packages**, then
create a policy under **Computers > Policies** with a **Packages** payload that installs
it, and scope it. The package is Developer ID signed and notarized, so there is no
Gatekeeper prompt. A newer version is a new package in the same policy; the fixed
package identifier `dk.nversion.agw-token` lets it replace the old files.

**Intune:** Intune's **macOS app (PKG)** and line-of-business app types detect a
successful install through the bundle identifiers of applications in the package. This
package installs command-line tools and no application bundle, so it can report as not
installed although it is. Deploy it from a shell script under **Devices > macOS > Scripts**
instead, which also verifies the download against the release checksums:

```sh
#!/bin/sh
set -eu
version=1.2.3
arch=$([ "$(uname -m)" = arm64 ] && echo arm64 || echo amd64)
base="https://github.com/nversion/agent-gateway-token-helper/releases/download/v$version"
cd "$(mktemp -d)"
curl -fsSLO "$base/agw-token-$version-darwin-$arch.pkg"
curl -fsSLO "$base/SHA256SUMS"
shasum -a 256 --check --ignore-missing SHA256SUMS
installer -pkg "agw-token-$version-darwin-$arch.pkg" -target /
```

Run it as root, once per device, and change the version to upgrade. Other Apple MDMs
(Kandji, Mosyle, Workspace ONE) install a signed `.pkg` through their custom package or
custom app feature. Use the `arm64` package on Apple Silicon; the `amd64` package also
installs there but runs through Rosetta.

**Machine-wide settings:** a `agw-token.json` in `/usr/local/bin` configures every user.
Deploy it with the same script, or with your MDM's file or script feature, owned by
root and world-readable.

### Linux

Extract the tarball into `/usr/local/bin` with your configuration management, or build a
distribution package from it. There is no signed package.

## Deploying the Claude Desktop settings

The [Claude Desktop guide](claude-desktop.md) and the
[setup generator](https://agw-token.nversion.dk/) give you a `.reg` file for Windows, a
`.mobileconfig` and a bare `.plist` for macOS, and a JSON file for Linux. Anthropic's
own configuration window can also export the same formats plus an ADMX template and a
profile manifest; see [Export the profile](https://claude.com/docs/third-party/claude-desktop/mdm#2-export-the-profile).

### Windows

Claude Desktop reads string and DWORD values directly under
`HKLM\SOFTWARE\Policies\Claude` (every user) or `HKCU\SOFTWARE\Policies\Claude` (one
user). Any value under `HKLM` makes the app ignore `HKCU` entirely, so deploy the whole
configuration to one hive, and never as `REG_EXPAND_SZ` or in a subkey, which the app
cannot read.

**Group Policy:** a GPO cannot import a `.reg` file directly. Either create the values as
registry items under **Computer Configuration > Preferences > Windows Settings >
Registry** (the Registry Wizard can read them from a machine where you imported the
file), or add the ADMX template exported by Claude Desktop to the central store and set
the values under **Administrative Templates**.

**Intune:** the simplest route is a PowerShell script under **Devices > Scripts and
remediations** that runs `reg import claude-gateway.reg` in the system context with the
64-bit host; a remediation script can also check the values on each run. The alternative
with reporting in the admin center is ADMX ingestion: import Claude Desktop's exported
ADMX template under **Devices > Configuration > Import ADMX**, then set the values in a
configuration profile of type **Imported Administrative templates**. User-targeted
assignments write to `HKCU`, so assign to devices for machine policy.

### macOS

The settings live in the `com.anthropic.claudefordesktop` preference domain, delivered
as a configuration profile at the computer level. The `.mobileconfig` from the generator
wraps the `.plist` in a profile with `PayloadScope` set to `System`.

**Jamf Pro:** upload the `.mobileconfig` under **Computers > Configuration Profiles >
Upload**. Jamf maps the payload to its own editor and may drop keys it does not know; if
the profile appears empty after upload, create it with the **Application & Custom
Settings** payload instead, choose **Upload**, enter the preference domain
`com.anthropic.claudefordesktop` and upload the bare `.plist`.

**Intune:** under **Devices > Configuration > Create > New policy > macOS**, either choose
**Templates > Custom** and upload the `.mobileconfig` with the device deployment
channel, or choose **Templates > Preference file**, enter the preference domain and
upload the `.plist`. Microsoft's page:
[Add preference file settings to macOS devices](https://learn.microsoft.com/en-us/intune/intune-service/configuration/preference-file-settings-macos).

**Kandji, Mosyle, Workspace ONE** and other Apple MDMs take the `.mobileconfig` as a
custom profile.

### Linux

Place `managed-settings.json` in `/etc/claude-desktop/` with your configuration
management. The file and directory must be owned by root, not symlinks and not writable
by anyone else, or Claude Desktop ignores them.

## Checking a device

On the device, confirm the helper and its configuration first:

```sh
agw-token version
agw-token status --provider entra --tenant contoso.onmicrosoft.com
```

`status` prints the configuration files in use. On macOS,
`sudo profiles show -type configuration` lists the installed profiles, and
`defaults read /Library/Managed\ Preferences/com.anthropic.claudefordesktop` prints the
managed values. On Windows, `reg query HKLM\SOFTWARE\Policies\Claude` prints them.

In Claude Desktop, **Help > Troubleshooting > Generate Diagnostic Report** writes a zip
whose `managed-config.txt` shows where each setting was read from, and the configuration
window opens read-only when a managed profile is in effect. The sign-in screen offers
Claude Desktop on 3P when the settings were read. If the app shows the standard sign-in
instead, the settings did not reach it; if it signs in but chats fail, check the
download host above.
