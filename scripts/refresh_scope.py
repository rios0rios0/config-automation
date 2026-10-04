"""Discover the exact documentation paths a repository refresh may edit."""

import json
from pathlib import Path, PurePosixPath
import subprocess
import sys


CREATABLE = (
    "CLAUDE.md",
    ".github/copilot-instructions.md",
    ".github/skills/code-review/SKILL.md",
)
EXCLUDED_DIRECTORIES = {
    ".git", ".venv", "venv", "node_modules", "vendor", "third_party",
    "third-party", "dist", "build", "target", "generated", "__pycache__",
}


def is_refresh_path(path):
    parts = PurePosixPath(path).parts
    if any(part in EXCLUDED_DIRECTORIES for part in parts[:-1]):
        return False
    if parts[-1] in {"CLAUDE.md", "AGENTS.md"}:
        return True
    if path in {"README.md", "CONTRIBUTING.md", ".github/CONTRIBUTING.md",
                "docs/CONTRIBUTING.md", ".github/copilot-instructions.md"}:
        return True
    for index, part in enumerate(parts[:-2]):
        section = parts[index + 1]
        if part == ".claude" and section in {"rules", "skills", "agents", "commands"}:
            return path.endswith(".md")
        if part == ".github":
            if section in {"skills", "agents"}:
                return path.endswith(".md")
            if section == "instructions":
                return path.endswith(".instructions.md")
            if section == "prompts":
                return path.endswith(".prompt.md")
    return False


def safe_file(root, path):
    # Permission rules are comma-delimited. Refuse ambiguous filenames rather
    # than allowing one to inject a second rule or become a wildcard grant.
    if any(ord(char) < 32 for char in path) or any(char in path for char in ",\\*?[]"):
        return False
    candidate = root
    parts = PurePosixPath(path).parts
    for index, part in enumerate(parts):
        candidate /= part
        if candidate.is_symlink():
            return False
        if index < len(parts) - 1 and candidate.exists() and not candidate.is_dir():
            return False
    return not candidate.exists() or candidate.is_file()


def discover(root):
    root = Path(root).resolve()
    entries = subprocess.check_output(
        ["git", "-C", str(root), "ls-files", "--stage", "-z"]
    ).decode("utf-8").split("\0")
    existing, skipped = set(), set()
    for entry in filter(None, entries):
        metadata, path = entry.split("\t", 1)
        mode, _, stage = metadata.split()
        if not is_refresh_path(path):
            continue
        if stage != "0":
            raise ValueError(f"unmerged refresh path: {path}")
        if mode not in {"100644", "100755"} or not safe_file(root, path):
            skipped.add(path)
            continue
        existing.add(path)
    creatable = {path for path in CREATABLE if safe_file(root, path)}
    paths = sorted(existing | creatable)
    # Root user-facing docs alone must not suppress initial agent guidance.
    human_docs = {"README.md", "CONTRIBUTING.md", ".github/CONTRIBUTING.md",
                  "docs/CONTRIBUTING.md"}
    return {
        "paths": paths,
        "existing": sorted(existing),
        "creatable": sorted(creatable),
        "has_guidance": bool(existing - human_docs),
        "allowed_tools": ["Read", "Grep", "Glob"] + [f"Edit(/{path})" for path in paths],
        "skipped": sorted(skipped),
    }


if __name__ == "__main__":
    json.dump(discover(sys.argv[1]), sys.stdout)
    print()
