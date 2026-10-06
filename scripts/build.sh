#!/usr/bin/env bash
# Build agw-token reproducibly. CI (.github/workflows/build.yaml) builds with this script,
# so it is the only place build flags live.
#
# Usage: scripts/build.sh [goos] [goarch]     (defaults to the host)
#
# Builds the plain binary and one per default-provider variant into build/, named like the
# release assets (agw-token-google-windows-amd64.exe), and prints their SHA-256 sums.
#
# The version stamped into the binary is $VERSION (a leading v is dropped), else the tag
# at HEAD without its v, else the first 12 characters of the commit. To reproduce release
# v1.2.3: git checkout v1.2.3 && scripts/build.sh linux amd64
#
# Linux and Windows builds are byte-for-byte reproducible: they use the exact Go version
# from go.mod, no cgo, -trimpath and no VCS stamping. macOS builds need cgo for the
# Keychain, and the C toolchain and SDK end up in the binary, so they are not.

# shellcheck source=scripts/release-lib.sh
source "$(dirname "$0")/release-lib.sh"

usage() { sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'; }
[[ "${1:-}" == "-h" || "${1:-}" == "--help" ]] && { usage; exit 0; }

need go git

go_version="$(awk '$1 == "go" { print $2; exit }' go.mod)"
[[ -n "$go_version" ]] || die "no go line in go.mod"

# Everything that changes the output is set explicitly rather than inherited.
unset GOFLAGS GOEXPERIMENT GOROOT
export GOTOOLCHAIN="go$go_version"
export GOOS="${1:-$(go env GOHOSTOS)}"
export GOARCH="${2:-$(go env GOHOSTARCH)}"
export GOAMD64=v1 GOARM64=v8.0
if [[ "$GOOS" == darwin ]]; then
    export CGO_ENABLED=1
else
    export CGO_ENABLED=0
fi

version="${VERSION:-}"
if [[ -z "$version" ]]; then
    version="$(git describe --tags --exact-match HEAD 2>/dev/null)" ||
        version="$(git rev-parse --short=12 HEAD)"
fi
version="${version#v}"

info "Building $GOOS/$GOARCH version $version with $GOTOOLCHAIN (cgo $CGO_ENABLED)"
mkdir -p build
built=()
for variant in "${VARIANTS[@]}"; do
    name="$(binary_name "$variant" "$GOOS-$GOARCH")"
    go build -trimpath -buildvcs=false \
        -ldflags "-s -w -X main.version=$version -X main.defaultProvider=$variant" \
        -o "build/$name" ./cmd/agw-token
    built+=("$name")
done
(cd build && shasum -a 256 "${built[@]}")
