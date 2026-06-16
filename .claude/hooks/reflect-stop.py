#!/usr/bin/env python3
"""Run agent-memoryd reflection from Stop hooks without blocking the agent."""

from __future__ import annotations

import json
import os
import shlex
import subprocess
import sys
import tempfile
import uuid
from pathlib import Path


DEFAULT_COMMAND = "codex exec --sandbox read-only --skip-git-repo-check --ephemeral -"


def main() -> int:
    if len(sys.argv) == 3 and sys.argv[1] == "--worker":
        return worker(Path(sys.argv[2]))

    if os.environ.get("AGENT_MEMORYD_HOOK_DEPTH"):
        print("{}")
        return 0

    raw = sys.stdin.read()
    try:
        event = json.loads(raw or "{}")
    except json.JSONDecodeError:
        event = {}

    cwd = Path(event.get("cwd") or os.getcwd())
    spool = cwd / ".agent-memoryd" / "hook-inputs"
    try:
        spool.mkdir(parents=True, exist_ok=True)
        input_path = spool / f"stop-{uuid.uuid4().hex}.json"
        input_path.write_text(raw, encoding="utf-8")
        log_file = open_log(cwd)
        subprocess.Popen(
            [sys.executable, __file__, "--worker", str(input_path)],
            stdout=log_file,
            stderr=log_file,
            cwd=str(cwd) if cwd.exists() else None,
            env=worker_env(),
            start_new_session=True,
        )
        log_file.close()
    except Exception as exc:
        log(cwd, f"reflect hook failed to spawn: {exc}")

    print("{}")
    return 0


def worker(input_path: Path) -> int:
    try:
        raw = input_path.read_text(encoding="utf-8")
        event = json.loads(raw or "{}")
    except Exception as exc:
        print(f"reflect worker could not read input: {exc}", file=sys.stderr)
        return 0
    finally:
        try:
            input_path.unlink()
        except Exception:
            pass

    cwd = event.get("cwd") or os.getcwd()
    command = shlex.split(os.environ.get("AGENT_MEMORYD_REFLECT_COMMAND", DEFAULT_COMMAND))
    timeout = int(os.environ.get("AGENT_MEMORYD_REFLECT_TIMEOUT", "300"))
    prompt = reflect_prompt(event)
    try:
        result = subprocess.run(
            command,
            input=prompt,
            text=True,
            capture_output=True,
            cwd=cwd if Path(cwd).exists() else None,
            env=worker_env(),
            timeout=timeout,
            check=False,
        )
    except Exception as exc:
        print(f"reflect worker failed to start command: {exc}", file=sys.stderr)
        return 0

    if result.stdout.strip():
        print(result.stdout.strip())
    if result.returncode != 0:
        print(result.stderr.strip() or "reflect command failed", file=sys.stderr)
    return 0


def reflect_prompt(event: dict) -> str:
    payload = {
        "transcript_path": event.get("transcript_path") or "",
        "cwd": event.get("cwd") or "",
        "source": f"hook:stop:{event.get('session_id') or 'unknown'}",
        "session": event.get("last_assistant_message") or "",
    }
    if payload["transcript_path"]:
        payload.pop("session", None)
    return f"""Call the agent-memoryd MCP `reflect` tool exactly once with this JSON input:
{json.dumps(payload, indent=2)}

Do not edit files or perform other work. If the MCP server or tool is unavailable, report the error and stop.
"""


def worker_env() -> dict:
    env = os.environ.copy()
    env["AGENT_MEMORYD_HOOK_DEPTH"] = "1"
    return env


def open_log(cwd: Path):
    try:
        log_dir = cwd / ".agent-memoryd"
        log_dir.mkdir(exist_ok=True)
        return (log_dir / "hooks.log").open("a")
    except Exception:
        return tempfile.TemporaryFile("a+")


def log(cwd: Path, message: str) -> None:
    try:
        with open_log(cwd) as f:
            f.write(message.rstrip() + "\n")
    except Exception:
        pass


if __name__ == "__main__":
    raise SystemExit(main())
