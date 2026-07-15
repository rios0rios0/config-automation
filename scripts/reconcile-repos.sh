#!/usr/bin/env bash
#
# reconcile-repos.sh — org-wide release reconciliation.
#
# For every target repository, detect CHANGELOG versions that were bumped but
# never released (their main-branch run failed the quality gate, so no tag/
# release was cut) and (re-)push the missing tag at its bump commit. Pushing the
# tag re-triggers the pipelines tag-push delivery path, which re-cuts the
# release. Detection is delegated to the pipelines primitive
# `global/scripts/shared/reconcile-releases.sh`; this script only enumerates the
# repos, clones them, pushes recovery tags, and reports.
#
# Environment:
#   GH_TOKEN          PAT that can list repos and push tags (Contents: write across
#                     the org). Must be a PAT, not GITHUB_TOKEN — a tag pushed with
#                     GITHUB_TOKEN does not start the delivery workflow.
#   RECONCILE_SCRIPT  Path to the pipelines reconcile-releases.sh detector.
#   HARDEN_OWNER      GitHub owner/org (default: rios0rios0).
#   REPOS_JSON        Optional pre-computed JSON array of {name,...}; when unset,
#                     falls back to the GitHub API.
#   DRY_RUN           "true" to detect and report without pushing tags.
#   FAIL_ON_GAP       "true" to exit non-zero if any unrecoverable gap remains.
set -euo pipefail

OWNER="${HARDEN_OWNER:-rios0rios0}"
DRY_RUN="${DRY_RUN:-false}"
FAIL_ON_GAP="${FAIL_ON_GAP:-false}"
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${RECONCILE_SCRIPT:?RECONCILE_SCRIPT (path to reconcile-releases.sh) is required}"

if [ ! -x "$RECONCILE_SCRIPT" ] && [ ! -f "$RECONCILE_SCRIPT" ]; then
  echo "ERROR: RECONCILE_SCRIPT not found at ${RECONCILE_SCRIPT}" >&2
  exit 1
fi

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/stdout}"

# Enumerate target repos. Prefer the JSON handed in by the workflow (from
# `harden-repos --list-json`); otherwise list the owner's non-fork, non-archived
# repos directly.
if [ -z "${REPOS_JSON:-}" ]; then
  # `gh repo list` resolves both user and org owners; the REST users/{owner}/repos
  # endpoint 404s for an org. --source excludes forks, --no-archived excludes archived.
  REPOS_JSON="$(gh repo list "$OWNER" --source --no-archived --limit 1000 --json name)"
fi
mapfile -t repos < <(printf '%s' "$REPOS_JSON" | jq -r '.[].name')

echo "Reconciling ${#repos[@]} repo(s) (dry_run=${DRY_RUN})."

rows=()
total_gaps=0
recovered=0
needs_review=0
repos_with_gaps=0
clone_failures=0

for name in "${repos[@]}"; do
  [ -n "$name" ] || continue
  dir="${WORKDIR}/${name}"
  # Blobless clone: full commit history (needed for the bump-commit lookup)
  # without downloading file blobs.
  if ! git clone --quiet --filter=blob:none \
       "https://x-access-token:${GH_TOKEN}@github.com/${OWNER}/${name}.git" "$dir" 2>/dev/null; then
    # A repo we could not clone was never scanned — surface it and count it toward
    # needs_review so FAIL_ON_GAP cannot pass while a repo went unchecked.
    echo "  skip ${name}: clone failed"
    clone_failures=$((clone_failures + 1))
    needs_review=$((needs_review + 1))
    rows+=("| \`${name}\` | — | — | clone failed — not scanned |")
    continue
  fi
  git -C "$dir" fetch --quiet --tags 2>/dev/null || true

  gaps="$(sh "$RECONCILE_SCRIPT" "$dir" 2>/dev/null || true)"
  [ -n "$gaps" ] || continue
  repos_with_gaps=$((repos_with_gaps + 1))

  while IFS="$(printf '\t')" read -r version sha status; do
    [ -n "$version" ] || continue
    total_gaps=$((total_gaps + 1))
    short_sha="$(printf '%s' "$sha" | cut -c1-8)"
    if [ "$status" = 'recoverable' ]; then
      if [ "$DRY_RUN" = 'true' ]; then
        action='would push tag (dry run)'
      elif git -C "$dir" tag "$version" "$sha" 2>/dev/null \
           && git -C "$dir" push --quiet origin "refs/tags/${version}" 2>/dev/null; then
        action='tag pushed — delivery re-triggered'
        recovered=$((recovered + 1))
      else
        action='tag push FAILED — investigate'
        needs_review=$((needs_review + 1))
      fi
    else
      action='needs manual review (no bump commit found)'
      needs_review=$((needs_review + 1))
    fi
    rows+=("| \`${name}\` | \`${version}\` | \`${short_sha}\` | ${action} |")
    echo "  ${name} ${version} (${short_sha}): ${action}"
  done <<EOF
$gaps
EOF
done

{
  echo "# Release reconciliation"
  echo ""
  echo "Scanned **${#repos[@]}** repo(s) (**${clone_failures}** failed to clone, not scanned); **${repos_with_gaps}** had gaps — **${total_gaps}** version(s): **${recovered}** recovered, **${needs_review}** need review."
  echo ""
  if [ "${#rows[@]}" -gt 0 ]; then
    echo "| Repo | Version | Bump commit | Action |"
    echo "| --- | --- | --- | --- |"
    printf '%s\n' "${rows[@]}"
  else
    echo "All repositories' changelogs agree with their tags. Nothing to reconcile."
  fi
} >> "$SUMMARY"

if [ "$FAIL_ON_GAP" = 'true' ] && [ "$needs_review" -gt 0 ]; then
  echo "FAIL: ${needs_review} unrecoverable gap(s) need manual review." >&2
  exit 1
fi
