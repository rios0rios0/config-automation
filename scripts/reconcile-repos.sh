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
# Only a version above the repository's highest SemVer tag is pushed. Every tag
# push deploys, so a gap at or below that tag (a bump that failed the gate and was
# superseded by a later release) would roll the deployment back to it. Such a gap
# is skipped and logged. Without this check the job re-pushed `medhub-life`'s
# superseded bumps every Monday (rios0rios0/config-automation#122).
#
# Environment:
#   GH_TOKEN          PAT that can list repos and push tags (Contents: write across
#                     the org). Must be a PAT, not GITHUB_TOKEN — a tag pushed with
#                     GITHUB_TOKEN does not start the delivery workflow.
#   RECONCILE_SCRIPT  Path to the pipelines reconcile-releases.sh detector.
#   HARDEN_OWNER      Comma-separated GitHub owners/orgs (default: rios0rios0).
#   REPOS_JSON        Optional pre-computed JSON array of {owner,name,...}; when
#                     unset, falls back to the GitHub API for every owner. An
#                     entry without `owner` is attributed to the first owner in
#                     HARDEN_OWNER, so older payloads keep working.
#   DRY_RUN           "true" to detect and report without pushing tags.
#   FAIL_ON_GAP       "true" to exit non-zero if any unrecoverable gap remains.
set -euo pipefail

IFS=',' read -r -a OWNERS <<< "${HARDEN_OWNER:-rios0rios0}"
# Trim whitespace around each entry and drop blanks, mirroring the Go CLI's
# parseOwners so a spaced-out list like 'a, b' behaves identically here.
_owners=()
for _owner in "${OWNERS[@]}"; do
  _owner="$(printf '%s' "$_owner" | tr -d '[:space:]')"
  [ -n "$_owner" ] && _owners+=("$_owner")
done
OWNERS=("${_owners[@]:-rios0rios0}")
DEFAULT_OWNER="${OWNERS[0]}"
DRY_RUN="${DRY_RUN:-false}"
FAIL_ON_GAP="${FAIL_ON_GAP:-false}"
: "${GH_TOKEN:?GH_TOKEN is required}"
: "${RECONCILE_SCRIPT:?RECONCILE_SCRIPT (path to reconcile-releases.sh) is required}"

if [ ! -x "$RECONCILE_SCRIPT" ] && [ ! -f "$RECONCILE_SCRIPT" ]; then
  echo "ERROR: RECONCILE_SCRIPT not found at ${RECONCILE_SCRIPT}" >&2
  exit 1
fi

# git authenticates through a credential helper that answers from GH_TOKEN when
# GitHub asks, as the config-and-docs refresh does. The token is therefore in no
# remote URL (which a clone stores in its .git/config and git can print in an
# error), on no command line, and in none of the git output this script logs. The
# helper is set in the environment rather than with `-c` so that it also reaches
# the detector's git commands, which fetch blobs from the blobless clone on
# demand. The empty value first drops any helper configured on the runner.
export GH_TOKEN
# shellcheck disable=SC2016 # GH_TOKEN is expanded by the shell git starts for the helper.
credential_helper='!f() { printf "username=x-access-token\npassword=%s\n" "${GH_TOKEN}"; }; f'
config_index="${GIT_CONFIG_COUNT:-0}"
export "GIT_CONFIG_KEY_${config_index}=credential.helper" "GIT_CONFIG_VALUE_${config_index}="
export "GIT_CONFIG_KEY_$((config_index + 1))=credential.helper" \
  "GIT_CONFIG_VALUE_$((config_index + 1))=${credential_helper}"
export GIT_CONFIG_COUNT="$((config_index + 2))"

# Version order, in jq. A tag counts when it is a SemVer version, with or without
# the leading `v` the detector also accepts: `X.Y.Z`, a pre-release such as
# `X.Y.Z-rc.1`, build metadata such as `X.Y.Z+5`, or the fork variant the detector
# emits, `X.Y.Z.N` or `X.Y.Z-N`. Any other tag (`v8`, `v2.5`, `latest`) is ignored.
# Versions compare by SemVer precedence: numerically per segment, so `0.10.0` is
# above `0.9.2` as a text sort would not have it; a pre-release below its release;
# build metadata ignored. The dotted fork segment ranks after the patch, so
# `X.Y.Z.0` equals `X.Y.Z`. The dashed `X.Y.Z-N` has two readings: SemVer reads it
# as a pre-release below `X.Y.Z`, the fork convention as `X.Y.Z.N`, above it. A
# gap must be above the highest tag under both, so an ambiguous one is skipped.
VERSION_ORDER_JQ='
def numeric: test("^[0-9]+$");
def parse:
  [capture("^v?(?<x>[0-9]+)[.](?<y>[0-9]+)[.](?<z>[0-9]+)([.](?<n>[0-9]+)|-(?<pre>[0-9A-Za-z-]+([.][0-9A-Za-z-]+)*))?([+][0-9A-Za-z-]+([.][0-9A-Za-z-]+)*)?$")]
  | first;
def key($fork):
  [.x, .y, .z | tonumber]
  + if .n != null then [1, (.n | tonumber)]
    elif .pre == null then [1, 0]
    elif $fork and (.pre | numeric) then [1, (.pre | tonumber)]
    else [0, (.pre | split(".") | map(if numeric then tonumber else . end))]
    end;
def floor:
  [.[] | {tag: ., p: parse} | select(.p != null)] as $tags
  | if $tags == [] then null
    else {semver: ($tags | map({tag, key: (.p | key(false))}) | max_by(.key)),
          fork: ($tags | map({tag, key: (.p | key(true))}) | max_by(.key))}
    end;
def verdict($floor):
  parse as $p
  | if $p == null then "is not a SemVer version"
    elif $floor == null then ""
    else first((false, true) as $fork
           | $floor[if $fork then "fork" else "semver" end] as $f
           | ($p | key($fork)) as $k
           | if $k < $f.key then "is below the highest tag \($f.tag)"
             elif $k == $f.key then "equals the highest tag \($f.tag)"
             else empty end) // ""
    end;
'

# skip_reason VERSION TAGS prints why VERSION must not be pushed to a repository
# whose tags are TAGS (one per line), or nothing when it is above every one.
skip_reason() {
  jq -rn --arg version "$1" --arg tags "$2" "${VERSION_ORDER_JQ}"'
    ($tags | split("\n") | map(select(length > 0)) | floor) as $floor
    | $version | verdict($floor)'
}

# print_indented TEXT prints the lines of git's output TEXT, if any, under the
# line they explain, without the padding git adds to a `remote:` line.
print_indented() {
  [ -n "$1" ] || return 0
  printf '%s\n' "$1" | sed -e 's/[[:space:]]*$//' -e 's/^/    /'
}

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT
SUMMARY="${GITHUB_STEP_SUMMARY:-/dev/stdout}"

# Enumerate target repos. Prefer the JSON handed in by the workflow (from
# `harden-repos --list-json`); otherwise list each owner's non-fork,
# non-archived repos directly.
if [ -z "${REPOS_JSON:-}" ]; then
  REPOS_JSON='[]'
  for owner in "${OWNERS[@]}"; do
    # `gh repo list` resolves both user and org owners; the REST users/{owner}/repos
    # endpoint 404s for an org. --source excludes forks, --no-archived excludes archived.
    batch="$(gh repo list "$owner" --source --no-archived --limit 1000 --json name \
             | jq -c --arg o "$owner" '[.[] | {owner: $o, name: .name}]')"
    REPOS_JSON="$(jq -cn --argjson a "$REPOS_JSON" --argjson b "$batch" '$a + $b')"
  done
fi
# Each entry becomes an `owner/name` slug so the loop below never has to guess
# which owner a bare name belongs to — two owners can host the same name.
mapfile -t repos < <(printf '%s' "$REPOS_JSON" \
  | jq -r --arg d "$DEFAULT_OWNER" '.[] | ((.owner // $d) + "/" + .name)')

echo "Reconciling ${#repos[@]} repo(s) across ${#OWNERS[@]} owner(s) (dry_run=${DRY_RUN})."

rows=()
total_gaps=0
recovered=0
skipped=0
needs_review=0
repos_with_gaps=0
scan_failures=0

for slug in "${repos[@]}"; do
  [ -n "$slug" ] || continue
  # Checkout directory keyed by the full slug (with the separator flattened) so
  # two owners hosting the same repo name do not collide in the work directory.
  dir="${WORKDIR}/${slug//\//__}"
  # Blobless clone: full commit history (needed for the bump-commit lookup)
  # without downloading file blobs.
  if ! clone_output="$(git clone --quiet --filter=blob:none \
       "https://github.com/${slug}.git" "$dir" 2>&1)"; then
    # A repo we could not clone was never scanned — surface it and count it toward
    # needs_review so FAIL_ON_GAP cannot pass while a repo went unchecked.
    echo "  skip ${slug}: clone failed"
    print_indented "$clone_output"
    scan_failures=$((scan_failures + 1))
    needs_review=$((needs_review + 1))
    rows+=("| \`${slug}\` | — | — | clone failed — not scanned |")
    continue
  fi
  # Every gap is compared with these tags, so a repo whose tags could not all be
  # fetched is not scanned rather than compared with a partial list.
  if ! fetch_output="$(git -C "$dir" fetch --quiet --tags 2>&1)"; then
    echo "  skip ${slug}: tag fetch failed"
    print_indented "$fetch_output"
    scan_failures=$((scan_failures + 1))
    needs_review=$((needs_review + 1))
    rows+=("| \`${slug}\` | — | — | tag fetch failed — not scanned |")
    continue
  fi
  tags="$(git -C "$dir" tag --list)"

  gaps="$(sh "$RECONCILE_SCRIPT" "$dir" 2>/dev/null || true)"
  [ -n "$gaps" ] || continue
  repos_with_gaps=$((repos_with_gaps + 1))

  while IFS="$(printf '\t')" read -r version sha status; do
    [ -n "$version" ] || continue
    total_gaps=$((total_gaps + 1))
    short_sha="$(printf '%s' "$sha" | cut -c1-8)"
    output=''
    # A gap that cannot be compared is skipped too: only a proven-higher version
    # is ever pushed.
    if ! reason="$(skip_reason "$version" "$tags")"; then
      reason='could not be compared with the tags'
    fi
    if [ -n "$reason" ]; then
      # Checked before the status, so a superseded gap without a bump commit is
      # not left for manual review either: nothing should ever push it.
      action="skipped: ${version} ${reason}"
      skipped=$((skipped + 1))
    elif [ "$status" = 'recoverable' ]; then
      if [ "$DRY_RUN" = 'true' ]; then
        action='would push tag (dry run)'
      elif output="$(git -C "$dir" tag "$version" "$sha" 2>&1 \
                     && git -C "$dir" push --quiet origin "refs/tags/${version}" 2>&1)"; then
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
    rows+=("| \`${slug}\` | \`${version}\` | \`${short_sha}\` | ${action} |")
    echo "  ${slug} ${version} (${short_sha}): ${action}"
    # git's own words for the tag and the push, success or failure: the reason a
    # push failed is in them.
    print_indented "$output"
  done <<EOF
$gaps
EOF
done

{
  echo "# Release reconciliation"
  echo ""
  echo "Scanned **${#repos[@]}** repo(s) across **${#OWNERS[@]}** owner(s) (**${scan_failures}** could not be cloned or fetched, not scanned); **${repos_with_gaps}** had gaps — **${total_gaps}** version(s): **${recovered}** recovered, **${skipped}** skipped as not above the highest tag, **${needs_review}** need review."
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
