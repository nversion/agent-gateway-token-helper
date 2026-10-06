#!/usr/bin/env bash
# Download the unsigned binaries that CI built for a release tag.
# Usage: scripts/release-download.sh <tag>
#
# Push the tag first (git tag v1.2.3 && git push origin v1.2.3). The script finds the
# Build workflow run for the tag's commit, waits for it if it is still running, and
# downloads the agw-token-<platform> artifacts into dist/<tag>/unsigned. For a public
# repository it checks each against the build provenance attestation the run made, so only
# binaries GitHub built from the tag get signed.

# shellcheck source=scripts/release-lib.sh
source "$(dirname "$0")/release-lib.sh"

release_init "${1:-}"
need gh

gh auth status >/dev/null 2>&1 || die "gh is not signed in, run: gh auth login"

repo="$(gh repo view --json nameWithOwner --jq .nameWithOwner)"
sha="$(gh api "repos/$repo/commits/$TAG" --jq .sha 2>/dev/null)" ||
    die "tag $TAG not found on GitHub, push it with: git push origin $TAG"
info "$TAG is commit $sha"

run_id="$(gh run list --repo "$repo" --workflow build.yaml --event push --branch "$TAG" \
    --json databaseId,headSha --jq "[.[] | select(.headSha == \"$sha\")][0].databaseId // empty")"
[[ -n "$run_id" ]] || die "no Build workflow run found for $TAG"
info "Build run $run_id: https://github.com/$repo/actions/runs/$run_id"

status="$(gh run view "$run_id" --repo "$repo" --json status --jq .status)"
if [[ "$status" != "completed" ]]; then
    info "Waiting for the run to finish"
    gh run watch "$run_id" --repo "$repo" --exit-status >/dev/null || true
fi
conclusion="$(gh run view "$run_id" --repo "$repo" --json conclusion --jq .conclusion)"
[[ "$conclusion" == "success" ]] || die "Build run $run_id concluded '$conclusion', not releasing it"

out="$DIST/unsigned"
download="$DIST/download"
rm -rf "$out" "$download"
mkdir -p "$out" "$download"
# Name each artifact rather than matching a prefix, so other artifacts of the run (such as
# the unsigned test MSIs) never end up among the binaries to sign.
names=()
for platform in "${PLATFORMS[@]}"; do
    names+=(--name "agw-token-$platform")
done
step gh run download "$run_id" --repo "$repo" --dir "$download" "${names[@]}"

# gh puts each artifact in a directory named after it, which is also the name of the
# plain binary inside, so move the files out into a separate directory.
find "$download" -type f -exec mv {} "$out/" \;
rm -rf "$download"

missing=0
for os in linux windows darwin; do
    while read -r name; do
        if [[ ! -f "$out/$name" ]]; then
            echo "missing: $name" >&2
            missing=1
        fi
    done < <(binaries_for "$os")
done
[[ "$missing" == 0 ]] || die "the run did not produce every expected binary"
chmod +x "$out"/agw-token-*

# The Build workflow only attests in a public repository: on a private one attestations
# need a paid GitHub plan.
if [[ "$(gh repo view "$repo" --json visibility --jq .visibility)" == "PUBLIC" ]]; then
    info "Checking build provenance attestations"
    for f in "$out"/agw-token-*; do
        gh attestation verify "$f" --repo "$repo" --source-digest "$sha" --source-ref "refs/tags/$TAG" \
            --signer-workflow "$repo/.github/workflows/build.yaml" >/dev/null ||
            die "$(basename "$f") has no valid attestation from the $TAG build"
    done
else
    info "$repo is not public, so the build has no attestations to check"
fi

cat >"$DIST/source.txt" <<EOF
tag=$TAG
commit=$sha
run=https://github.com/$repo/actions/runs/$run_id
EOF

# The native binary must report the tag's version.
arch="$(uname -m)"
[[ "$arch" == "x86_64" ]] && arch=amd64
native="$out/$(binary_name "" "darwin-$arch")"
if [[ "$(uname -s)" == "Darwin" && -x "$native" ]]; then
    got="$("$native" version)"
    [[ "$got" == *"$VERSION"* ]] || die "$native reports version '$got', expected $VERSION"
    info "$(basename "$native") version: $got"
fi

info "Downloaded $(find "$out" -type f | wc -l | tr -d ' ') binaries to $out"
