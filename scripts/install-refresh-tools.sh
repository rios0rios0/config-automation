#!/usr/bin/env bash
# Installs what a refresh leg runs, the same way on every CI: the Claude Code
# CLI, a checksum-verified chlog, and refresh-docs built from this checkout.
# It is the one place their versions are pinned. Usage:
#
#   scripts/install-refresh-tools.sh <bin-directory>
#
# chlog and refresh-docs land in <bin-directory>, which the caller adds to
# PATH in its own CI's dialect; npm installs the CLI globally.
set -euo pipefail

bin_directory="${1:?usage: install-refresh-tools.sh <bin-directory>}"
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"

# /checkup prompt-audit arrived in 2.1.283. The range stays open so new
# releases keep reaching the refresh, which is why the Node.js major the CI
# installs has to keep up with the CLI's own engines floor.
claude_code_range='>=2.1.283'
# `chlog new` is the only way a refresh may write a changelog fragment: the
# grants allow that command and no write to the fragment directory, so the
# fragment's name and `time` come from the tool's clock. A pinned release,
# verified against the checksum it publishes, keeps every leg on one version.
chlog_version='0.4.0'
chlog_sha256='c3f825869482b397455b0ae5016d17397f900ccfa61adba038a45bea6600c329'

mkdir -p -- "${bin_directory}"

npm install -g "@anthropic-ai/claude-code@${claude_code_range}"

scratch="$(mktemp -d)"
trap 'rm -rf -- "${scratch}"' EXIT
archive="chlog-${chlog_version}-linux-amd64.tar.gz"
curl -fsSL --retry 3 -o "${scratch}/${archive}" \
  "https://github.com/luizjhonata/chlog/releases/download/v${chlog_version}/${archive}"
echo "${chlog_sha256}  ${scratch}/${archive}" | sha256sum --check --strict
tar -xzf "${scratch}/${archive}" -C "${bin_directory}" chlog
"${bin_directory}/chlog" --version

(cd -- "${repository_root}" && go build -o "${bin_directory}/refresh-docs" ./cmd/refresh-docs)
