# Vendored tools

## jsign

`jsign-7.5.jar` is [Jsign](https://github.com/ebourg/jsign) 7.5, used by
`scripts/release-sign.sh` to Authenticode-sign the Windows binaries through Azure
Artifact Signing from macOS. Microsoft's own tools (signtool, `dotnet sign`,
AzureSignTool) only run on Windows.

It needs a Java runtime (8 or later): `brew install openjdk` (the sign script finds it
without linking) or `brew install --cask temurin`.

The jar is the release asset from
<https://github.com/ebourg/jsign/releases/download/7.5/jsign-7.5.jar>. Its SHA-1 matches
Maven Central's `net.jsign:jsign:7.5`. `jsign-7.5.jar.sha256` holds the checksum, and the
sign script refuses to run if the jar does not match it.

To update: download the new jar, check it against Maven Central's `.sha1`, write a new
`.sha256` file with `shasum -a 256 jsign-X.Y.jar > jsign-X.Y.jar.sha256`, delete the old
files, and update `JSIGN_JAR` in `scripts/release-lib.sh`.

## Microsoft Identity Verification Root Certificate Authority 2020

`microsoft-identity-verification-root-2020.pem` is the root that Azure Artifact Signing
certificates and timestamps chain to. Windows trusts it, but the OpenSSL bundle on macOS
does not include it, so `scripts/release-sign.sh` passes it to `osslsigncode verify`.
It is converted to PEM from
<https://www.microsoft.com/pkiops/certs/Microsoft%20Identity%20Verification%20Root%20Certificate%20Authority%202020.crt>.

SHA-1 fingerprint: `F4:00:42:E2:E5:F7:E8:EF:81:89:FE:D1:55:19:AE:CE:42:C3:BF:A2`
