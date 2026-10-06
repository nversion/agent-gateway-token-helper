# shellcheck shell=bash
# The globals set here are used by the scripts that source this file.
# shellcheck disable=SC2034

# Shared helpers for the release scripts. Source it, don't run it.
#
# Release flow (all run on the Mac mini, each takes the tag as first argument):
#   1. scripts/release-download.sh <tag>  fetch the CI build of the tag into dist/<tag>/unsigned
#   2. scripts/release-sign.sh <tag> ...  sign, notarize and package into dist/<tag>/release
#   3. scripts/release-upload.sh <tag>    upload dist/<tag>/release to a (draft) GitHub release

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# GOOS-GOARCH pairs built by the build job in .github/workflows/build.yaml.
PLATFORMS=(linux-amd64 linux-arm64 windows-amd64 windows-arm64 darwin-amd64 darwin-arm64)

# Default-provider variants built per platform ("" is the plain binary).
VARIANTS=("" google entra)

JSIGN_JAR="tools/jsign-7.5.jar"

# Root of the Artifact Signing certificate and timestamp chains, for osslsigncode verify.
MS_ROOT_CA="tools/microsoft-identity-verification-root-2020.pem"

die() {
    echo "error: $*" >&2
    exit 1
}

info() {
    echo "==> $*"
}

step() {
    echo "==> $*"
    "$@"
}

need() {
    local cmd
    for cmd in "$@"; do
        command -v "$cmd" >/dev/null 2>&1 || die "$cmd not found on PATH"
    done
}

# release_init <tag> sets TAG, VERSION and DIST.
release_init() {
    TAG="${1:-}"
    [[ -n "$TAG" ]] || die "missing tag argument, e.g. v1.2.3"
    [[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?$ ]] ||
        die "tag must look like v1.2.3 or v1.2.3-rc1, got $TAG"
    VERSION="${TAG#v}"
    DIST="dist/$TAG"
}

# binary_name <variant> <platform> prints the CI artifact name, e.g.
# agw-token-google-darwin-arm64 or agw-token-windows-amd64.exe.
binary_name() {
    local variant="$1" platform="$2" ext=""
    [[ "$platform" == windows-* ]] && ext=".exe"
    echo "agw-token${variant:+-$variant}-${platform}${ext}"
}

# short_name <variant> <platform> prints the name used inside archives, e.g.
# agw-token-google or agw-token.exe.
short_name() {
    local variant="$1" platform="$2" ext=""
    [[ "$platform" == windows-* ]] && ext=".exe"
    echo "agw-token${variant:+-$variant}${ext}"
}

# binaries_for <os> prints the CI artifact names for every platform of that OS.
binaries_for() {
    local os="$1" platform variant
    for platform in "${PLATFORMS[@]}"; do
        [[ "$platform" == "$os"-* ]] || continue
        for variant in "${VARIANTS[@]}"; do
            binary_name "$variant" "$platform"
        done
    done
}
