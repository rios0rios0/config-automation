"""Keep AGENTS.md as the instruction file and every CLAUDE.md as an import of it."""

import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import sys

from refresh_scope import POINTER, ignored, is_pair_path, is_pointer, is_refresh_path, safe_parents


LEGACY_BANNER = ("# CLAUDE.md\n\nThis file provides guidance to Claude Code (claude.ai/code) "
                 "when working with code in this repository.\n")
BANNER = ("# AGENTS.md\n\nThis file provides guidance to AI coding agents (Claude Code, Codex, "
          "Cursor, GitHub Copilot) when working with code in this repository.\n")
MIGRATED_HEADING = "## Migrated from CLAUDE.md"
FENCE = re.compile(r" {0,3}(`{3,}|~{3,})")
RENAME_MESSAGE = "chore(refresh): renamed `CLAUDE.md` to `AGENTS.md`"
# One line per outcome for the refresh prompt, in reporting order.
NOTES = {
    "renamed": ("- Renamed `{path}` to `AGENTS.md` beside it in a separate commit, so history follows"
                + " the file, and recreated it as the import line."),
    "merged": ("- Appended the former `{path}` text to the `AGENTS.md` beside it under `{heading}`,"
               + " then reduced `{path}` to the import line."),
    "pointed": "- Added `{path}` importing the existing `AGENTS.md` beside it.",
    "rebannered": "- Replaced the legacy `# CLAUDE.md` banner of `{path}`.",
    "dangling": ("- `{path}` imports an `AGENTS.md` that does not exist; create it if that directory"
                 + " needs guidance."),
    "skipped": ("- Left `{path}` and its `AGENTS.md` unchanged (a symlink to another file, an ignored"
                + " path, or not UTF-8 text); report it for human review."),
}


def git(root, *args):
    return subprocess.run(["git", "-C", str(root), *args], check=True,
                          capture_output=True, text=True).stdout


def read(root, path):
    return (root / path).read_bytes().decode("utf-8")


def side(root, path, sibling):
    """Classify one file of a pair without following a symlink."""
    target = root / path
    if not safe_parents(root, path):
        return "unsafe"
    if ignored(root, path):
        return "ignored"
    if target.is_symlink():
        return "link" if PurePosixPath(os.readlink(target)) == PurePosixPath(sibling) else "unsafe"
    if not target.exists():
        return "absent"
    if not target.is_file():
        return "unsafe"
    try:
        text = read(root, path)
    except UnicodeDecodeError:
        return "unsafe"
    return "pointer" if sibling == "AGENTS.md" and is_pointer(text) else "file"


def pairs(root):
    # Untracked files count too: the second pass must see an AGENTS.md the
    # refresh created.
    listed = git(root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
    directories = {PurePosixPath(path).parent for path in filter(None, listed.split("\0"))
                   if is_pair_path(path) and is_refresh_path(path)}
    for directory in sorted(directories):
        claude, agents = str(directory / "CLAUDE.md"), str(directory / "AGENTS.md")
        yield claude, agents, (side(root, claude, "AGENTS.md"), side(root, agents, "CLAUDE.md"))


def without_imports(text):
    # Claude Code ignores an import inside a code block, so an example of one stays.
    kept, fence = [], ""
    for line in text.splitlines():
        marker = FENCE.match(line)
        if marker and not fence:
            fence = marker[1]
        elif marker and line.strip() == marker[1] and marker[1].startswith(fence):
            fence = ""
        elif not fence and is_pointer(line):
            continue
        kept.append(line)
    return "\n".join(kept)


def content_lines(text):
    return [line.rstrip() for line in text.splitlines() if line.strip()]


def contains(text, extra):
    # Compare whole lines in order: a substring test finds "run make test."
    # inside "Do not run make test." and would drop the conflicting line.
    haystack, needle = content_lines(text), content_lines(extra)
    return any(haystack[index:index + len(needle)] == needle
               for index in range(len(haystack) - len(needle) + 1))


def merge(root, claude, agents):
    # Keep everything CLAUDE.md adds; the refresh integrates it afterwards.
    extra = without_imports(read(root, claude).removeprefix(LEGACY_BANNER)).strip()
    text = read(root, agents)
    if extra and not contains(text, extra):
        text = f"{text.rstrip()}\n\n{MIGRATED_HEADING}\n\n{extra}\n"
        (root / agents).write_bytes(text.encode("utf-8"))
    (root / claude).write_bytes(POINTER.encode("utf-8"))
    return "merged"


def point(root, claude, agents):
    # Replace a symlink itself rather than writing through it.
    if (root / claude).is_symlink():
        (root / claude).unlink()
    (root / claude).write_bytes(POINTER.encode("utf-8"))
    return "pointed"


def dangle(*_):
    return "dangling"


def keep(*_):
    return None


def skip(*_):
    return "skipped"


HANDLERS = {
    ("file", "file"): merge,
    ("absent", "file"): point,
    ("link", "file"): point,
    ("pointer", "absent"): dangle,
    ("pointer", "file"): keep,
    ("absent", "absent"): keep,
}


def describe(report):
    lines = [NOTES[outcome].format(path=path, heading=MIGRATED_HEADING)
             for outcome in NOTES for path in report[outcome]]
    return "\n".join(lines) or "- No changes: every `CLAUDE.md` already imports the `AGENTS.md` beside it."


def normalize(root):
    root = Path(root).resolve()
    report = {outcome: [] for outcome in NOTES}
    # Renames are committed alone so `git log --follow AGENTS.md` reaches the
    # history CLAUDE.md had; every other change waits for the refresh commit.
    # A pair with an ignored or unsafe side never reaches a handler that writes.
    for claude, agents, state in list(pairs(root)):
        if state in {("file", "absent"), ("file", "link")}:
            if state[1] == "link":
                git(root, "rm", "-q", "--", agents)
            git(root, "mv", "--", claude, agents)
            report["renamed"].append(claude)
    if report["renamed"]:
        git(root, "commit", "-q", "-m", RENAME_MESSAGE)
    touched = []
    for claude, agents, state in list(pairs(root)):
        handler = HANDLERS.get(state, skip)
        outcome = handler(root, claude, agents)
        if outcome and not (outcome == "pointed" and claude in report["renamed"]):
            report[outcome].append(claude)
        if outcome == "skipped":
            continue
        if outcome in {"merged", "pointed"}:
            touched += [claude, agents]
        if state[1] == "file" and read(root, agents).startswith(LEGACY_BANNER):
            text = BANNER + read(root, agents).removeprefix(LEGACY_BANNER)
            (root / agents).write_bytes(text.encode("utf-8"))
            touched.append(agents)
            report["rebannered"].append(agents)
    if touched:
        git(root, "add", "--", *sorted(set(touched)))
    return dict(report, changed=bool(report["renamed"] or touched), note=describe(report))


if __name__ == "__main__":
    json.dump(normalize(sys.argv[1]), sys.stdout)
    print()
