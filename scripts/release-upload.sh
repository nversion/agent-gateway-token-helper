#!/usr/bin/env bash
# Upload the files made by release-sign.sh to a GitHub release.
#
# Usage: scripts/release-upload.sh <tag> [flags]
#   --publish          publish the release; without it the release stays a draft
#   --allow-unsigned   upload even if macOS or Windows signing was skipped
#   --force            replace assets on a release that is already published
#   --only OSES        upload only these platforms, comma separated from linux, windows
#                      and darwin, e.g. --only windows. The uploaded SHA256SUMS and
#                      SHA256SUMS-unsigned then cover just those files; a later full upload
#                      replaces them. No attestation is requested.
#   --no-attest        do not run the "Attest signed release" workflow
#
# The release is created as a draft with generated notes if it does not exist yet.
# Tags with a suffix such as v1.2.3-rc1 are marked as prereleases.

# shellcheck source=scripts/release-lib.sh
source "$(dirname "$0")/release-lib.sh"

usage() { sed -n '2,/^$/p' "$0" | sed 's/^# \{0,1\}//'; }
[[ "${1:-}" == "-h" || "${1:-}" == "--help" ]] && { usage; exit 0; }

release_init "${1:-}"
shift

publish=0
allow_unsigned=0
force=0
only=""
attest=1
while [[ $# -gt 0 ]]; do
    case "$1" in
    --publish) publish=1; shift ;;
    --allow-unsigned) allow_unsigned=1; shift ;;
    --force) force=1; shift ;;
    --only) only="${2:?}"; shift 2 ;;
    --no-attest) attest=0; shift ;;
    -h | --help) usage; exit 0 ;;
    *) die "unknown flag $1" ;;
    esac
done

need gh shasum
gh auth status >/dev/null 2>&1 || die "gh is not signed in, run: gh auth login"

release="$DIST/release"
[[ -f "$release/SHA256SUMS" ]] || die "$release/SHA256SUMS not found, run scripts/release-sign.sh $TAG first"
[[ -f "$release/SHA256SUMS-unsigned" ]] ||
    die "$release/SHA256SUMS-unsigned not found, re-run scripts/release-sign.sh $TAG"

info "Checking SHA256SUMS"
(cd "$release" && shasum -a 256 --check --quiet SHA256SUMS) || die "files in $release do not match SHA256SUMS"
if [[ -f "$DIST/notarized.sha256" ]]; then
    info "Checking the macOS zips and packages are the ones Apple notarized"
    (cd "$release" && shasum -a 256 --check --quiet "$ROOT/$DIST/notarized.sha256") ||
        die "macOS zips or packages changed after notarization"
fi

oses=(linux windows darwin)
if [[ -n "$only" ]]; then
    IFS=, read -r -a oses <<<"$only"
    for os in "${oses[@]}"; do
        [[ "$os" =~ ^(linux|windows|darwin)$ ]] || die "--only takes linux, windows or darwin, got $os"
    done
fi

if [[ "$allow_unsigned" == 0 ]]; then
    for os in "${oses[@]}"; do
        signed_as="$os"
        [[ "$os" == darwin ]] && signed_as=macos
        [[ "$os" == linux ]] && continue
        grep -qx "$signed_as" "$DIST/signing.txt" 2>/dev/null ||
            die "$os binaries were not signed; re-run release-sign.sh or pass --allow-unsigned"
    done
fi

prerelease=()
[[ "$VERSION" == *-* ]] && prerelease=(--prerelease)

if is_draft="$(gh release view "$TAG" --json isDraft --jq .isDraft 2>/dev/null)"; then
    if [[ "$is_draft" != "true" && "$force" == 0 ]]; then
        die "release $TAG is already published; pass --force to replace its assets"
    fi
    info "Updating existing release $TAG"
else
    step gh release create "$TAG" --verify-tag --draft --generate-notes --title "$TAG" ${prerelease[@]+"${prerelease[@]}"}
fi

assets=()
if [[ -z "$only" ]]; then
    while IFS= read -r -d '' f; do assets+=("$f"); done < <(find "$release" -maxdepth 1 -type f -print0 | sort -z)
else
    # Matches both archives (agw-token-1.2.3-windows-amd64.zip) and binaries
    # (agw-token-google-windows-amd64.exe).
    partial="$DIST/partial"
    rm -rf "$partial"
    mkdir -p "$partial"
    for os in "${oses[@]}"; do
        while IFS= read -r -d '' f; do assets+=("$f"); done < <(find "$release" -maxdepth 1 -type f -name "*-$os-*" -print0 | sort -z)
    done
    [[ ${#assets[@]} -gt 0 ]] || die "no files for $only in $release"
    for f in "${assets[@]}"; do
        awk -v name="$(basename "$f")" '$2 == name' "$release/SHA256SUMS"
    done >"$partial/SHA256SUMS"
    for os in "${oses[@]}"; do
        awk -v os="$os" 'index($2, "-" os "-")' "$release/SHA256SUMS-unsigned"
    done >"$partial/SHA256SUMS-unsigned"
    assets+=("$partial/SHA256SUMS" "$partial/SHA256SUMS-unsigned")
fi
info "Uploading ${#assets[@]} files"
gh release upload "$TAG" "${assets[@]}" --clobber

if [[ "$attest" == 1 && -z "$only" ]]; then
    # The workflow file is taken from the default branch, the code it runs from the tag.
    step gh workflow run attest-signed.yaml -f tag="$TAG"
    info "Attestation requested, follow it with: gh run list --workflow attest-signed.yaml"
elif [[ -n "$only" ]]; then
    info "Partial upload, not attesting; a full upload requests the attestation"
fi

if [[ "$publish" == 1 ]]; then
    step gh release edit "$TAG" --draft=false
fi

url="$(gh release view "$TAG" --json url --jq .url)"
if [[ "$publish" == 1 ]]; then
    info "Published $url"
else
    info "Draft release ready for review: $url"
    info "Publish it with: scripts/release-upload.sh $TAG --publish"
fi
