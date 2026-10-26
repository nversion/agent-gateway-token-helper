#!/usr/bin/env bash
# Sign, notarize and package the binaries downloaded by release-download.sh.
#
# Usage: scripts/release-sign.sh <tag> [flags]
#
# macOS (Developer ID, needs the certificate in the login keychain):
#   --apple-identity NAME    codesign identity, e.g. "Developer ID Application: Name (TEAMID)"
#   --apple-installer-identity NAME
#                            productbuild identity for the .pkg,
#                            e.g. "Developer ID Installer: Name (TEAMID)"
#   --notary-profile NAME    notarytool keychain profile, created once with
#                            xcrun notarytool store-credentials NAME --apple-id ... --team-id ...
#   --apple-bundle-id ID     code signing identifier for every macOS binary
#                            (default dk.nversion.agw-token)
# Windows (Azure Artifact Signing through the vendored jsign, needs az login):
#   --azure-endpoint URL     e.g. https://weu.codesigning.azure.net
#   --azure-account NAME     Artifact Signing account name
#   --azure-profile NAME     certificate profile name
# Other:
#   --skip-macos             leave the macOS binaries unsigned
#   --skip-windows           leave the Windows binaries unsigned
#
# Linux binaries are packaged as they are. Output goes to dist/<tag>/release:
#   agw-token-<version>-darwin-<arch>.zip    notarized, uploaded byte for byte as submitted
#   agw-token-<version>-darwin-<arch>.pkg    installs into /usr/local/bin, built from the
#                                            signed executables, then signed, notarized
#                                            and stapled
#   agw-token-<version>-windows-<arch>.zip
#   agw-token-<version>-windows-<arch>.msi   installs into C:\Program Files\Agent Gateway helper,
#                                            built from the signed executables, then signed
#   agw-token-<version>-linux-<arch>.tar.gz
#   the individual signed binaries under their CI names, and SHA256SUMS.
#   SHA256SUMS-unsigned, the sums of the binaries as CI built them, which is what
#   users get from scripts/build.sh and what cmd/verify-build checks downloads against.

# shellcheck source=scripts/release-lib.sh
source "$(dirname "$0")/release-lib.sh"

usage() { sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'; }
[[ "${1:-}" == "-h" || "${1:-}" == "--help" ]] && { usage; exit 0; }

release_init "${1:-}"
shift

apple_identity=""
apple_installer_identity=""
notary_profile=""
bundle_id="dk.nversion.agw-token"
azure_endpoint=""
azure_account=""
azure_profile=""
sign_macos=1
sign_windows=1

while [[ $# -gt 0 ]]; do
    case "$1" in
    --apple-identity) apple_identity="${2:?}"; shift 2 ;;
    --apple-installer-identity) apple_installer_identity="${2:?}"; shift 2 ;;
    --notary-profile) notary_profile="${2:?}"; shift 2 ;;
    --apple-bundle-id) bundle_id="${2:?}"; shift 2 ;;
    --azure-endpoint) azure_endpoint="${2:?}"; shift 2 ;;
    --azure-account) azure_account="${2:?}"; shift 2 ;;
    --azure-profile) azure_profile="${2:?}"; shift 2 ;;
    --skip-macos) sign_macos=0; shift ;;
    --skip-windows) sign_windows=0; shift ;;
    -h | --help) usage; exit 0 ;;
    *) die "unknown flag $1" ;;
    esac
done

if [[ "$sign_macos" == 1 ]]; then
    [[ -n "$apple_identity" ]] || die "--apple-identity is required (or pass --skip-macos)"
    [[ -n "$apple_installer_identity" ]] || die "--apple-installer-identity is required (or pass --skip-macos)"
    [[ -n "$notary_profile" ]] || die "--notary-profile is required (or pass --skip-macos)"
fi
if [[ "$sign_windows" == 1 ]]; then
    [[ -n "$azure_endpoint" ]] || die "--azure-endpoint is required (or pass --skip-windows)"
    [[ -n "$azure_account" ]] || die "--azure-account is required (or pass --skip-windows)"
    [[ -n "$azure_profile" ]] || die "--azure-profile is required (or pass --skip-windows)"
fi

[[ -d "$DIST/unsigned" ]] || die "$DIST/unsigned not found, run scripts/release-download.sh $TAG first"

# msitools builds the Windows Installer packages (scripts/build-msi.sh) and extracts
# them again to check the executables inside; pkgbuild and productbuild build the macOS
# packages (scripts/build-pkg.sh) and pkgutil expands them again.
need shasum zip tar ditto go wixl msibuild msiinfo msiextract pkgbuild productbuild pkgutil

# Check every prerequisite before signing anything.
if [[ "$sign_windows" == 1 ]]; then
    need az
    # macOS ships a java stub that fails without a registered JDK, and Homebrew's
    # openjdk is keg-only, so fall back to it explicitly.
    java="java"
    if ! java -version >/dev/null 2>&1; then
        java="$(brew --prefix openjdk 2>/dev/null)/bin/java"
        "$java" -version >/dev/null 2>&1 ||
            die "no Java runtime found, install one with: brew install openjdk"
    fi
    (cd tools && shasum -a 256 --check --status "$(basename "$JSIGN_JAR").sha256") ||
        die "$JSIGN_JAR does not match its checksum"
fi
if [[ "$sign_macos" == 1 ]]; then
    need codesign xcrun security jq
    security find-identity -v -p codesigning | grep -qF "\"$apple_identity\"" ||
        die "codesign identity \"$apple_identity\" not found in the keychain"
    # Installer identities are not valid for the codesigning policy, so list them all.
    security find-identity -v | grep -qF "\"$apple_installer_identity\"" ||
        die "installer identity \"$apple_installer_identity\" not found in the keychain"
fi

signed="$DIST/signed"
stage="$DIST/stage"
release="$DIST/release"
rm -rf "$signed" "$stage" "$release" "$DIST/notarized.sha256" "$DIST/signing.txt"
mkdir -p "$signed" "$stage" "$release"
cp -p "$DIST/unsigned"/agw-token-* "$signed/"
(cd "$DIST/unsigned" && shasum -a 256 agw-token-*) >"$release/SHA256SUMS-unsigned"

# --- Windows ---------------------------------------------------------------

if [[ "$sign_windows" == 1 ]]; then
    info "Getting an Azure access token for Artifact Signing"
    AZURE_ACCESS_TOKEN="$(az account get-access-token --resource https://codesigning.azure.net \
        --query accessToken --output tsv)" || die "az could not get a token, run: az login"
    export AZURE_ACCESS_TOKEN

    # authenticode <description> <file>... signs Windows executables or installer
    # packages; Windows shows the description in the UAC prompt. One
    # invocation for all files keeps within the service's request quota. The token is
    # read from the environment so it never shows up in the process list.
    authenticode() {
        local description="$1"
        shift
        "$java" -jar "$JSIGN_JAR" \
            --storetype TRUSTEDSIGNING \
            --keystore "$azure_endpoint" \
            --storepass env:AZURE_ACCESS_TOKEN \
            --alias "$azure_account/$azure_profile" \
            --tsaurl http://timestamp.acs.microsoft.com \
            --tsmode RFC3161 \
            --alg SHA-256 \
            --name "$description" \
            --url https://github.com/nversion/agent-gateway-token-helper \
            --replace \
            "$@"
    }

    # verify_authenticode <file>... checks the signatures when osslsigncode is installed.
    verify_authenticode() {
        local file
        if ! command -v osslsigncode >/dev/null 2>&1; then
            info "osslsigncode not installed, skipping signature verification (brew install osslsigncode)"
            return
        fi
        for file in "$@"; do
            # Artifact Signing chains to a Microsoft root that Windows trusts but the
            # OpenSSL bundle on macOS does not include, so pass it explicitly.
            step osslsigncode verify -CAfile "$MS_ROOT_CA" -TSA-CAfile "$MS_ROOT_CA" \
                -in "$file" >/dev/null
        done
    }

    exes=()
    while read -r name; do exes+=("$signed/$name"); done < <(binaries_for windows)

    info "Signing ${#exes[@]} Windows binaries"
    authenticode agw-token "${exes[@]}"
    verify_authenticode "${exes[@]}"
    # Signing must only have added a signature, or the release cannot be checked
    # against a reproducible build.
    step go run ./cmd/verify-build "$release/SHA256SUMS-unsigned" "${exes[@]}" >/dev/null
    echo "windows" >>"$DIST/signing.txt"
fi

# --- macOS -----------------------------------------------------------------

if [[ "$sign_macos" == 1 ]]; then
    while read -r name; do
        # A fixed identifier keeps the designated requirement the same for every variant
        # and release, so Keychain items stay readable without an "allow" prompt.
        step codesign --force --options runtime --timestamp \
            --identifier "$bundle_id" --sign "$apple_identity" "$signed/$name"
        codesign --verify --strict --verbose=2 "$signed/$name"
    done < <(binaries_for darwin)
fi

# --- Packaging -------------------------------------------------------------

# stage_platform <platform> copies the platform's binaries into a staging directory
# under their short names and prints the directory.
stage_platform() {
    local platform="$1" dir="$stage/$1" variant
    mkdir -p "$dir"
    for variant in "${VARIANTS[@]}"; do
        cp -p "$signed/$(binary_name "$variant" "$platform")" "$dir/$(short_name "$variant" "$platform")"
    done
    cp LICENSE README.md "$dir/"
    echo "$dir"
}

for platform in "${PLATFORMS[@]}"; do
    dir="$(stage_platform "$platform")"
    base="agw-token-$VERSION-$platform"
    case "$platform" in
    linux-*)
        COPYFILE_DISABLE=1 tar --no-mac-metadata --no-xattrs --uid 0 --gid 0 \
            --uname root --gname root -czf "$release/$base.tar.gz" -C "$dir" .
        ;;
    windows-*)
        (cd "$dir" && zip -q -X -r "$ROOT/$release/$base.zip" .)
        scripts/build-msi.sh "$platform" "$VERSION" "$dir" "$release/$base.msi" >/dev/null
        ;;
    darwin-*)
        ditto -c -k --norsrc --noextattr --noacl "$dir" "$release/$base.zip"
        pkg_sign=()
        [[ "$sign_macos" == 1 ]] && pkg_sign=(--sign "$apple_installer_identity")
        scripts/build-pkg.sh "$platform" "$VERSION" "$dir" "$release/$base.pkg" \
            ${pkg_sign[@]+"${pkg_sign[@]}"} >/dev/null
        ;;
    esac
    info "Packaged $base"
done

msis=()
for platform in "${PLATFORMS[@]}"; do
    if [[ "$platform" == windows-* ]]; then
        msis+=("$release/agw-token-$VERSION-$platform.msi")
    fi
done
if [[ "$sign_windows" == 1 ]]; then
    info "Signing ${#msis[@]} Windows Installer packages"
    authenticode "Agent Gateway helper" "${msis[@]}"
    verify_authenticode "${msis[@]}"
fi
unset AZURE_ACCESS_TOKEN

# The packages must hold exactly the executables in the zip, which verify-build traces
# back to the reproducible build.
for platform in "${PLATFORMS[@]}"; do
    [[ "$platform" == windows-* ]] || continue
    msi="$release/agw-token-$VERSION-$platform.msi"
    extracted="$stage/msi-$platform"
    msiextract -C "$extracted" "$msi" >/dev/null
    contents=()
    while IFS= read -r -d '' f; do contents+=("$f"); done < <(find "$extracted" -name '*.exe' -print0)
    [[ ${#contents[@]} -eq ${#VARIANTS[@]} ]] ||
        die "$(basename "$msi") holds ${#contents[@]} executables, expected ${#VARIANTS[@]}"
    for f in "${contents[@]}"; do
        cmp -s "$f" "$stage/$platform/$(basename "$f")" ||
            die "$(basename "$msi"): $(basename "$f") differs from the packaged executable"
    done
    step go run ./cmd/verify-build "$release/SHA256SUMS-unsigned" "${contents[@]}" >/dev/null
done

# The macOS packages must hold exactly the executables in the zip. They cannot be traced
# to a reproducible build, so they are compared with the signed binaries instead.
for platform in "${PLATFORMS[@]}"; do
    [[ "$platform" == darwin-* ]] || continue
    pkg="$release/agw-token-$VERSION-$platform.pkg"
    extracted="$stage/pkg-$platform"
    pkgutil --expand-full "$pkg" "$extracted"
    contents=()
    while IFS= read -r -d '' f; do contents+=("$f"); done < <(find "$extracted" -path '*/Payload/usr/local/bin/*' -type f -print0)
    [[ ${#contents[@]} -eq ${#VARIANTS[@]} ]] ||
        die "$(basename "$pkg") holds ${#contents[@]} executables, expected ${#VARIANTS[@]}"
    for f in "${contents[@]}"; do
        cmp -s "$f" "$stage/$platform/$(basename "$f")" ||
            die "$(basename "$pkg"): $(basename "$f") differs from the packaged executable"
    done
done

if [[ "$sign_macos" == 1 ]]; then
    # notarize <file> submits the file and waits for Apple's verdict.
    notarize() {
        local path="$1" result id status
        info "Notarizing $(basename "$path") (this usually takes a few minutes)"
        result="$(xcrun notarytool submit "$path" --keychain-profile "$notary_profile" \
            --wait --output-format json)" || true
        id="$(jq -r '.id // empty' <<<"$result")"
        status="$(jq -r '.status // empty' <<<"$result")"
        if [[ "$status" != "Accepted" ]]; then
            echo "$result" >&2
            [[ -n "$id" ]] && xcrun notarytool log "$id" --keychain-profile "$notary_profile" >&2
            die "notarization of $(basename "$path") failed with status '${status:-unknown}'"
        fi
        info "Accepted, submission $id"
    }

    for platform in "${PLATFORMS[@]}"; do
        [[ "$platform" == darwin-* ]] || continue
        zip_path="$release/agw-token-$VERSION-$platform.zip"
        pkg_path="$release/agw-token-$VERSION-$platform.pkg"
        # A zip cannot carry the notarization ticket, so Gatekeeper fetches it online
        # and the zip is uploaded exactly as submitted. A package can, so the ticket
        # is stapled to it and the stapled file is what gets uploaded.
        notarize "$zip_path"
        notarize "$pkg_path"
        step xcrun stapler staple -q "$pkg_path"
        spctl --assess --type install "$pkg_path" || die "Gatekeeper rejects $(basename "$pkg_path")"
        # release-upload.sh checks that these exact bytes are what gets published.
        (cd "$release" && shasum -a 256 "$(basename "$zip_path")" "$(basename "$pkg_path")") >>"$DIST/notarized.sha256"
    done
    codesign -dvv "$signed/$(binary_name "" darwin-arm64)" 2>&1 | grep -E '^(Identifier|Authority|TeamIdentifier|Timestamp)='
    pkgutil --check-signature "$release/agw-token-$VERSION-darwin-arm64.pkg" | grep -E 'Status|Developer ID'
    echo "macos" >>"$DIST/signing.txt"
fi

cp -p "$signed"/agw-token-* "$release/"
(cd "$release" && shasum -a 256 agw-token-* >SHA256SUMS)
rm -rf "$stage"

[[ "$sign_macos" == 1 ]] || info "WARNING: macOS binaries are NOT signed"
[[ "$sign_windows" == 1 ]] || info "WARNING: Windows binaries are NOT signed"
info "Release files are in $release"
