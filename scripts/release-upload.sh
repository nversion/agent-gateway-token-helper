#!/usr/bin/env bash
# Upload the files made by release-sign.sh to a GitHub release.
#
# Usage: scripts/release-upload.sh <tag> [flags]
#   --publish          publish the release; without it the release stays a draft
#   --allow-unsigned   upload even if macOS or Windows signing was skipped
#   --force            replace assets on a release that is already published
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
while [[ $# -gt 0 ]]; do
    case "$1" in
    --publish) publish=1; shift ;;
    --allow-unsigned) allow_unsigned=1; shift ;;
    --force) force=1; shift ;;
    -h | --help) usage; exit 0 ;;
    *) die "unknown flag $1" ;;
    esac
done

need gh shasum
gh auth status >/dev/null 2>&1 || die "gh is not signed in, run: gh auth login"

release="$DIST/release"
[[ -f "$release/SHA256SUMS" ]] || die "$release/SHA256SUMS not found, run scripts/release-sign.sh $TAG first"

info "Checking SHA256SUMS"
(cd "$release" && shasum -a 256 --check --quiet SHA256SUMS) || die "files in $release do not match SHA256SUMS"
if [[ -f "$DIST/notarized.sha256" ]]; then
    info "Checking the macOS zips are the ones Apple notarized"
    (cd "$release" && shasum -a 256 --check --quiet "$ROOT/$DIST/notarized.sha256") ||
        die "macOS zips changed after notarization"
fi

if [[ "$allow_unsigned" == 0 ]]; then
    for os in macos windows; do
        grep -qx "$os" "$DIST/signing.txt" 2>/dev/null ||
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
while IFS= read -r -d '' f; do assets+=("$f"); done < <(find "$release" -maxdepth 1 -type f -print0 | sort -z)
info "Uploading ${#assets[@]} files"
gh release upload "$TAG" "${assets[@]}" --clobber

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
