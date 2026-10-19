"""Discover the exact documentation paths a repository refresh may edit."""

import json
from pathlib import Path, PurePosixPath
import subprocess
import sys


CREATABLE = (
    "AGENTS.md",
    ".github/copilot-instructions.md",
    ".github/skills/code-review/SKILL.md",
)
EXCLUDED_DIRECTORIES = {
    ".git", ".venv", "venv", "node_modules", "vendor", "third_party",
    "third-party", "dist", "build", "target", "generated", "__pycache__",
}
# AGENTS.md holds the instructions; a CLAUDE.md beside it only imports them.
POINTER = "@AGENTS.md\n"


def is_pointer(text):
    return text.strip() in {"@AGENTS.md", "@./AGENTS.md"}


def is_pair_path(path):
    # `.claude/CLAUDE.md` is Claude Code's alternative project location, not
    # one side of an AGENTS.md pair.
    parts = PurePosixPath(path).parts
    return parts[-1] in {"CLAUDE.md", "AGENTS.md"} and ".claude" not in parts[:-1]


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


def safe_parents(root, path):
    # Permission rules are comma-delimited. Refuse ambiguous filenames rather
    # than allowing one to inject a second rule or become a wildcard grant.
    if any(ord(char) < 32 for char in path) or any(char in path for char in ",\\*?[]"):
        return False
    candidate = root
    for part in PurePosixPath(path).parts[:-1]:
        candidate /= part
        if candidate.is_symlink() or (candidate.exists() and not candidate.is_dir()):
            return False
    return True


def safe_file(root, path):
    candidate = root / path
    return (safe_parents(root, path) and not candidate.is_symlink()
            and (not candidate.exists() or candidate.is_file()))


def ignored(root, path):
    # A file the repository ignores is deliberately absent; never create it.
    # `--no-index` matches the patterns even while the path is still tracked.
    return subprocess.run(["git", "-C", str(root), "check-ignore", "--no-index", "-q", "--", path],
                          check=False).returncode == 0


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
    # Pointers stay in the manifest for drift and staging, but only the host
    # writes them, so they never receive an edit grant. The root one is listed
    # even while absent: the host adds it once the refresh creates AGENTS.md.
    managed = {path for path in existing if is_pair_path(path)
               and PurePosixPath(path).name == "CLAUDE.md"
               and is_pointer((root / path).read_text(encoding="utf-8", errors="replace"))}
    existing -= managed
    if "CLAUDE.md" not in existing and safe_file(root, "CLAUDE.md"):
        managed.add("CLAUDE.md")
    # A pointer whose AGENTS.md is missing may have that file created.
    siblings = {str(PurePosixPath(path).parent / "AGENTS.md") for path in managed}
    creatable = {path for path in (set(CREATABLE) | siblings) - existing
                 if safe_file(root, path) and not ignored(root, path)}
    editable = existing | creatable
    # Root user-facing docs alone must not suppress initial agent guidance.
    human_docs = {"README.md", "CONTRIBUTING.md", ".github/CONTRIBUTING.md",
                  "docs/CONTRIBUTING.md"}
    return {
        "paths": sorted(editable | managed),
        "existing": sorted(existing),
        "creatable": sorted(creatable),
        "managed": sorted(managed),
        "has_guidance": bool(existing - human_docs),
        "allowed_tools": ["Read", "Grep", "Glob"] + [f"Edit(/{path})" for path in sorted(editable)],
        "skipped": sorted(skipped),
    }


if __name__ == "__main__":
    json.dump(discover(sys.argv[1]), sys.stdout)
    print()
