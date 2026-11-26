#!/bin/bash

#
# Release script for the markdownweb Go module on GitHub using gh cli.
# A module release is a tag and release notes; there are no binaries to upload.
#
# Usage: release.bash [--dry-run]
#   --dry-run  do everything up to the prompt (notes), say what would be created
#              and from which commit, and change nothing.
#
# The release is made from the commit HEAD (workspace DR-0012). The script commits
# and pushes nothing: it stops unless the tree is committed and HEAD is pushed
# (make check-committed), and creates the draft with --target HEAD. GitHub makes the
# tag, on that commit, when you finalize the draft.
#
# Running it again after an interruption is safe (workspace DR-0013). A draft for
# the tag that already exists and targets HEAD is complete, since a module release
# has no files to upload: the script creates nothing and says to finalize it. A
# published release, or a draft for another commit, stops the script.
#
DRY_RUN=0
for ARG in "$@"; do
    case "${ARG}" in
        -n|--dry-run) DRY_RUN=1 ;;
        -h|--help) sed -n '2,/^DRY_RUN/p' "$0" | grep '^#'; exit 0 ;;
        *) echo "usage: release.bash [--dry-run]" >&2; exit 2 ;;
    esac
done

# The notes file is removed however the script ends, even if the terminal is lost.
trap 'rm -f release_notes.tmp' EXIT

REPO_ID="${PWD##*/}"
GROUP_ID="$(git config --get remote.origin.url | sed -E 's#.*github\.com[:/]([^/]+)/.*#\1#')"
REPO_URL="https://github.com/${GROUP_ID}/${REPO_ID}"
echo "REPO_URL -> ${REPO_URL}"

#
# Generate a new draft release using jq and gh
#
RELEASE_TAG="v$(jq -r .version codemeta.json)"
if ! printf '%s' "${RELEASE_TAG}" | grep -qE '^v[0-9a-zA-Z._-]+$'; then
    echo "error: version contains unexpected characters: ${RELEASE_TAG}"
    exit 1
fi
echo "tag: ${RELEASE_TAG}, notes:"
jq -r .releaseNotes codemeta.json >release_notes.tmp
cat release_notes.tmp

# The release is built from one known commit. Stop unless it is committed and
# pushed.
if ! make -s check-committed; then
    exit 1
fi
RELEASE_COMMIT="$(git rev-parse HEAD)"

# A release that already exists is a draft for this commit or it stops the run, so a
# second release is never created and a published one is never touched.
if VIEW="$(gh release view "${RELEASE_TAG}" --json isDraft,targetCommitish 2>/dev/null)"; then
    if [ "$(printf '%s' "${VIEW}" | jq -r .isDraft)" != "true" ]; then
        echo "error: ${RELEASE_TAG} is not a draft on GitHub; it is published and is not touched"
        exit 1
    fi
    DRAFT_TARGET="$(printf '%s' "${VIEW}" | jq -r .targetCommitish)"
    if [ "${DRAFT_TARGET}" != "${RELEASE_COMMIT}" ]; then
        echo "error: the draft for ${RELEASE_TAG} targets ${DRAFT_TARGET}, not HEAD ${RELEASE_COMMIT}; delete the draft first"
        exit 1
    fi
    echo "The draft ${RELEASE_TAG} already exists and targets ${RELEASE_COMMIT}; nothing to create or upload."
    echo "Now finalize the draft: ${REPO_URL}/releases"
    exit 0
fi

echo "New release: draft ${RELEASE_TAG} at ${RELEASE_COMMIT} (no files to upload)"
if [ "${DRY_RUN}" = "1" ]; then
    echo "dry run: nothing changed"
    exit 0
fi
# shellcheck disable=SC2162
read -r -p "Create the draft release on GitHub with gh? (y/N) " YES_NO
if [ "${YES_NO}" = "y" ]; then
    echo "Pushing release ${RELEASE_TAG} to GitHub"
    gh release create "${RELEASE_TAG}" \
        --draft \
        --target "${RELEASE_COMMIT}" \
        -F release_notes.tmp \
        --generate-notes || exit 1

    cat <<EOT

Now goto repo release and finalize draft.

	${REPO_URL}/releases

EOT
fi
