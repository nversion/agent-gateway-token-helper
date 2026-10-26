#!/usr/bin/env bash
# Build the macOS installer package for one architecture with pkgbuild and productbuild.
# release-sign.sh and CI build with this script, so it is the only place pkg flags live;
# the package layout is in packaging/macos/distribution.xml.
#
# Usage: scripts/build-pkg.sh <platform> <version> <dir> <out.pkg> [--sign IDENTITY]
#   platform  darwin-amd64 or darwin-arm64
#   version   the release version, e.g. 1.2.3 or 1.2.3-rc1 (a leading v is dropped)
#   dir       directory with agw-token, agw-token-google, agw-token-entra, LICENSE
#             and README.md
#   --sign    "Developer ID Installer: Name (TEAMID)" identity from the login keychain;
#             without it the package is unsigned (CI, or --skip-macos releases)
#
# The package installs the executables into /usr/local/bin and the documents into
# /usr/local/share/agw-token, for every user of the machine. The arm64 package only
# installs on Apple Silicon; the amd64 one also installs there, through Rosetta.
#
# The package itself is not reproducible (the payload archive and bill of materials hold
# timestamps), but the executables inside are the signed binaries: extract them with
# pkgutil --expand-full and compare.

# release-lib.sh changes to the repository root, so make the paths absolute first.
abs() { [[ "$1" == /* ]] && echo "$1" || echo "$PWD/$1"; }
[[ $# -ge 4 ]] && set -- "$1" "$2" "$(abs "$3")" "$(abs "$4")" "${@:5}"

# shellcheck source=scripts/release-lib.sh
source "$(dirname "$0")/release-lib.sh"

usage() { sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'; }
[[ "${1:-}" == "-h" || "${1:-}" == "--help" ]] && { usage; exit 0; }
[[ $# -ge 4 ]] || { usage >&2; exit 2; }

platform="$1" version="${2#v}" dir="$3" out="$4"
shift 4
identity=""
while [[ $# -gt 0 ]]; do
    case "$1" in
    --sign) identity="${2:?}"; shift 2 ;;
    *) die "unknown flag $1" ;;
    esac
done

case "$platform" in
darwin-amd64) archs="x86_64,arm64" ;;
darwin-arm64) archs="arm64" ;;
*) die "platform must be darwin-amd64 or darwin-arm64, got $platform" ;;
esac

[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.+-]+)?$ ]] ||
    die "version must look like 1.2.3 or 1.2.3-rc1, got $version"

for f in agw-token agw-token-google agw-token-entra LICENSE README.md; do
    [[ -f "$dir/$f" ]] || die "$dir/$f not found"
done

need pkgbuild productbuild

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
root="$work/root"
mkdir -p "$root/usr/local/bin" "$root/usr/local/share/agw-token"
for f in agw-token agw-token-google agw-token-entra; do
    cp -p "$dir/$f" "$root/usr/local/bin/$f"
    chmod 755 "$root/usr/local/bin/$f"
done
cp -p "$dir/LICENSE" "$dir/README.md" "$root/usr/local/share/agw-token/"
chmod 644 "$root/usr/local/share/agw-token"/*

# The component package holds the files; the product archive around it carries the
# title, the host architectures and the signature.
step pkgbuild --root "$root" \
    --identifier dk.nversion.agw-token \
    --version "$version" \
    --install-location / \
    --ownership recommended \
    "$work/agw-token.pkg" >/dev/null

sed -e "s/@VERSION@/$version/g" -e "s/@ARCHS@/$archs/g" \
    packaging/macos/distribution.xml >"$work/distribution.xml"

sign=()
[[ -n "$identity" ]] && sign=(--sign "$identity" --timestamp)
mkdir -p "$(dirname "$out")"
rm -f "$out"
step productbuild --distribution "$work/distribution.xml" \
    --package-path "$work" \
    ${sign[@]+"${sign[@]}"} \
    "$out" >/dev/null

if [[ -n "$identity" ]]; then
    pkgutil --check-signature "$out" | grep -qF "$identity" ||
        die "$out is not signed by $identity"
fi
info "Built $out"
