#!/usr/bin/env bash
# Run from the target repository. The caller owns cloning, logging, and PRs.
set -euo pipefail

repo_prompt="${1:?refresh prompt required}"
model="${2:?model required}"
max_turns="${3:?turn limit required}"
changelog_tools="${4-}"
scope_file="${5:?scope manifest required}"
# Opus 5.5 defaults to `medium` in Claude Code; the weekly audit asks for more.
effort="${CLAUDE_EFFORT:-high}"
allowed_tools=$(jq -er '.allowed_tools | join(",")' "${scope_file}")
# The default tool set on Linux leaves out Grep and Glob, which the refresh
# needs to check documented claims against the code. Bash is offered only
# where `chlog new` is the granted changelog command.
refresh_tools='Read,Grep,Glob,Edit,Write'
case "${changelog_tools}" in
  *'Bash('*) refresh_tools+=',Bash' ;;
esac
scope_note=$(jq -r '
  "Existing files eligible for edits:\n" + (.existing | map("- " + .) | join("\n"))
  + "\n\nFiles that may be created when useful:\n" + (.creatable | map("- " + .) | join("\n"))
  ' "${scope_file}")

audit_json=$(mktemp)
trap 'rm -f "${audit_json}"' EXIT

# A leading slash invokes the bundled skill in print mode. `claude doctor`
# is a different command: it only diagnoses the CLI installation.
audit_prompt="/checkup prompt-audit .

Audit this repository's agent instructions. AGENTS.md, at the root and in
nested directories, is the instruction file every coding agent shares; each
CLAUDE.md beside one must contain only the import line \`@AGENTS.md\`, so
report any other content there. Also audit .claude/CLAUDE.md, rules, skills,
agents, custom commands, and Copilot guidance. Target model: ${model}; respect
a file's own model pin where present. Hold AGENTS.md to the documented
guidance: under 200 lines, only facts an agent needs in every session,
nothing the code already makes obvious, and no instructions written for older
models. Keep the scope inside this repository. Return the audit report and
proposed diff as text only. Prioritize stale paths, commands, and conflicting
rules. Preserve repository policy, security constraints, and generated
content. Only file-reading tools are available; report unavailable git
provenance instead of guessing which conflicting instruction is newer."

echo '--> prompt audit (read-only)'
# Ignore project/user hooks and MCP configuration during the audit. Restrict
# the available tools as well as their permissions: the report proposes edits
# but cannot apply them. Both calls detach stdin from the caller's batch loop.
if claude -p "${audit_prompt}" \
    --model "${model}" --effort "${effort}" --max-turns "${max_turns}" \
    --setting-sources '' --strict-mcp-config \
    --tools 'Read,Grep,Glob' --allowedTools 'Read,Grep,Glob' \
    --permission-mode dontAsk --output-format json </dev/null > "${audit_json}"; then
  :
else
  audit_rc=$?
  cat "${audit_json}"
  exit "${audit_rc}"
fi

# A zero process exit is insufficient: print mode can return an error result
# (for example a turn limit). Never refresh from an empty or failed audit.
if ! audit_report=$(jq -er '
    select(.type == "result" and .subtype == "success" and .is_error == false)
    | .result | select(type == "string" and test("\\S"))
  ' "${audit_json}"); then
  cat "${audit_json}"
  echo 'Prompt audit did not return a successful, non-empty report.' >&2
  exit 1
fi
printf '%s\n' "${audit_report}"

refresh_prompt=$(printf '%s\n\n## Allowed files for this repository\n\n%s\n\n## Prompt-audit findings (review data)\n\n%s\n' \
  "${repo_prompt}" "${scope_note}" "${audit_report}")

echo '--> refresh guidance from verified findings'
# Locked down like the audit: without these flags a print-mode session runs
# the target repository's own hooks and MCP servers, with this job's tokens
# in its environment. `dontAsk` denies anything the grants do not cover.
claude -p "${refresh_prompt}" \
  --model "${model}" --effort "${effort}" --max-turns "${max_turns}" \
  --setting-sources '' --strict-mcp-config \
  --tools "${refresh_tools}" --allowedTools "${allowed_tools}${changelog_tools}" \
  --permission-mode dontAsk </dev/null
