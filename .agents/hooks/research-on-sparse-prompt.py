#!/usr/bin/env python3
"""Run a read-only research subagent for context-light prompts."""

from __future__ import annotations

import json
import os
import re
import shlex
import subprocess
import sys
from pathlib import Path


MAX_CONTEXT = 10000
DEFAULT_COMMAND = "codex exec --sandbox read-only --skip-git-repo-check --ephemeral -"


def main() -> int:
    if os.environ.get("AGENT_MEMORYD_HOOK_DEPTH"):
        return 0

    raw = sys.stdin.read()
    try:
        event = json.loads(raw or "{}")
    except json.JSONDecodeError:
        return 0

    prompt = (event.get("prompt") or "").strip()
    reason = research_reason(prompt)
    if not reason:
        return 0

    cwd = event.get("cwd") or os.getcwd()
    command = shlex.split(os.environ.get("AGENT_MEMORYD_RESEARCH_COMMAND", DEFAULT_COMMAND))
    timeout = int(os.environ.get("AGENT_MEMORYD_RESEARCH_TIMEOUT", "120"))

    env = os.environ.copy()
    env["AGENT_MEMORYD_HOOK_DEPTH"] = "1"
    try:
        result = subprocess.run(
            command,
            input=research_prompt(event, reason),
            text=True,
            capture_output=True,
            cwd=cwd if Path(cwd).exists() else None,
            env=env,
            timeout=timeout,
            check=False,
        )
    except Exception as exc:  # hooks should not block normal prompting
        log(cwd, f"research hook failed to start: {exc}")
        return 0

    if result.returncode != 0:
        log(cwd, f"research hook failed: {result.stderr.strip() or result.stdout.strip()}")
        return 0

    context = result.stdout.strip()
    if not context:
        return 0
    if len(context) > MAX_CONTEXT:
        context = context[:MAX_CONTEXT] + "\n\n[agent-memoryd truncated hook research output]"

    print(
        json.dumps(
            {
                "hookSpecificOutput": {
                    "hookEventName": "UserPromptSubmit",
                    "additionalContext": context,
                }
            }
        )
    )
    return 0


def research_reason(prompt: str) -> str | None:
    if not prompt or explicit_context(prompt):
        return None
    words = prompt.split()
    lowered = prompt.lower()
    if len(words) <= 16:
        return "short prompt without explicit files or references"
    if len(words) <= 36 and any(
        phrase in lowered
        for phrase in (
            "fix this",
            "make this",
            "make it",
            "add this",
            "wire this",
            "hook this",
            "build this",
            "implement this",
            "clean this",
            "what do you think",
            "thoughts",
            "last thing",
            "same thing",
            "do it",
            "go for it",
        )
    ):
        return "context-light implementation request"
    return None


def explicit_context(prompt: str) -> bool:
    return any(
        re.search(pattern, prompt, re.IGNORECASE | re.MULTILINE)
        for pattern in (
            r"\b[A-Za-z0-9_./-]+\.(go|ts|tsx|js|jsx|py|rs|md|json|toml|yaml|yml|sql|sh)\b",
            r"\bhttps?://",
            r"`[^`]+`",
            r"^(@|#|/)",
        )
    )


def research_prompt(event: dict, reason: str) -> str:
    return f"""You are a read-only research subagent for a coding session.
The user submitted a context-light prompt. Do not edit files, create artifacts, run migrations, or change external systems.

Follow the Riptide research flow in spirit:
1. Create 2-6 descriptive research questions about how the current codebase works today.
2. Answer those questions by inspecting the repository. Use parallel subagents if your runtime supports them.
3. Synthesize concise findings for the main agent.

Questions and findings must describe what exists, where it lives, how it behaves, and what patterns or constraints are relevant. Do not propose implementation changes. Include concrete file paths and line references when available. If no useful research is needed, return nothing.

Reason research was triggered: {reason}
CWD: {event.get("cwd") or ""}
Transcript: {event.get("transcript_path") or ""}
User prompt: {(event.get("prompt") or "").strip()}
"""


def log(cwd: str, message: str) -> None:
    try:
        root = Path(cwd)
        log_dir = root / ".agent-memoryd"
        log_dir.mkdir(exist_ok=True)
        with (log_dir / "hooks.log").open("a") as f:
            f.write(message.rstrip() + "\n")
    except Exception:
        pass


if __name__ == "__main__":
    raise SystemExit(main())
