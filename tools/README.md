# Vendored tools

## jsign

`jsign-7.5.jar` is [Jsign](https://github.com/ebourg/jsign) 7.5, used by
`scripts/release-sign.sh` to Authenticode-sign the Windows binaries through Azure
Artifact Signing from macOS. Microsoft's own tools (signtool, `dotnet sign`,
AzureSignTool) only run on Windows.

It needs a Java runtime (8 or later): `brew install --cask temurin`.

The jar is the release asset from
<https://github.com/ebourg/jsign/releases/download/7.5/jsign-7.5.jar>. Its SHA-1 matches
Maven Central's `net.jsign:jsign:7.5`. `jsign-7.5.jar.sha256` holds the checksum, and the
sign script refuses to run if the jar does not match it.

To update: download the new jar, check it against Maven Central's `.sha1`, write a new
`.sha256` file with `shasum -a 256 jsign-X.Y.jar > jsign-X.Y.jar.sha256`, delete the old
files, and update `JSIGN_JAR` in `scripts/release-lib.sh`.
