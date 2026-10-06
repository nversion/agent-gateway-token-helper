#!/usr/bin/env bash
# Build the Windows Installer package for one architecture with wixl (msitools).
# release-sign.sh and CI build with this script, so it is the only place MSI flags live;
# the package layout is in packaging/windows/agw-token.wxs.
#
# Usage: scripts/build-msi.sh <platform> <version> <dir> <out.msi>
#   platform  windows-amd64 or windows-arm64
#   version   the release version, e.g. 1.2.3 or 1.2.3-rc1 (a leading v is dropped)
#   dir       directory with agw-token.exe, agw-token-google.exe, agw-token-entra.exe,
#             LICENSE and README.md
#
# wixl (msitools 0.106) cannot target arm64 yet, so the arm64 package is built as x64
# and then marked Arm64 in its summary information. The two only differ there: both
# use 64-bit components and install into C:\Program Files.
#
# Windows Installer versions are numeric, so a suffix such as -rc1 is dropped from the
# product version; the executables still carry the full version.
#
# The MSI itself is not reproducible (the cabinet and the summary information hold
# timestamps and the product code is random), but the executables inside are the
# reproducible build: extract them with msiextract and check them with cmd/verify-build.

# release-lib.sh changes to the repository root, so make the paths absolute first.
abs() { [[ "$1" == /* ]] && echo "$1" || echo "$PWD/$1"; }
[[ $# -eq 4 ]] && set -- "$1" "$2" "$(abs "$3")" "$(abs "$4")"

# shellcheck source=scripts/release-lib.sh
source "$(dirname "$0")/release-lib.sh"

usage() { sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'; }
[[ "${1:-}" == "-h" || "${1:-}" == "--help" ]] && { usage; exit 0; }
[[ $# -eq 4 ]] || { usage >&2; exit 2; }

platform="$1" version="${2#v}" dir="$3" out="$4"

case "$platform" in
windows-amd64) template="x64;1033" ;;
windows-arm64) template="Arm64;1033" ;;
*) die "platform must be windows-amd64 or windows-arm64, got $platform" ;;
esac

product_version="${version%%[-+]*}"
[[ "$product_version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] ||
    die "version must start with a.b.c, got $version"

for f in agw-token.exe agw-token-google.exe agw-token-entra.exe LICENSE README.md; do
    [[ -f "$dir/$f" ]] || die "$dir/$f not found"
done

need wixl msibuild msiinfo

# new_guid prints a random GUID in the upper-case form Windows Installer uses.
new_guid() {
    if command -v uuidgen >/dev/null 2>&1; then
        uuidgen
    else
        cat /proc/sys/kernel/random/uuid
    fi | tr '[:lower:]' '[:upper:]'
}

mkdir -p "$(dirname "$out")"
rm -f "$out"
step wixl --arch x64 \
    --define "Version=$product_version" \
    --define "SourceDir=$dir" \
    --output "$out" \
    packaging/windows/agw-token.wxs

# wixl ignores Permanent="no", so the PATH entry would survive an uninstall. The -
# prefix removes the appended directory again when the component is removed.
step msibuild "$out" -q "UPDATE \`Environment\` SET \`Name\` = '=-*PATH' WHERE \`Name\` = '=*PATH'"

if [[ "$template" != "x64;1033" ]]; then
    # Rewriting the summary information needs every field; the package code must be
    # new, since it identifies this exact package.
    step msibuild "$out" -s "Agent Gateway helper $product_version" nversion "$template" \
        "{$(new_guid)}"
fi

msiinfo export "$out" Environment | grep -q $'\t=-\\*PATH\t' || die "PATH entry in $out is not removed on uninstall"
msiinfo suminfo "$out" | grep -qxF "Template: $template" || die "$out is not marked $template"
info "Built $out"
