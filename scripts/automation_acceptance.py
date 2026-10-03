#!/usr/bin/env python3
"""Execute generated command hooks, with an isolated synthetic store.

This verifies the adapter contract, not a real model's lesson quality or a host's
hook approval UI. It never opens a transcript or contacts a model provider.
"""
import json
import os
import pathlib
import shlex
import shutil
import subprocess
import sys
import tempfile

os.environ["ELEPHANT_UPDATE_CHECKS"] = "0"

with tempfile.TemporaryDirectory(prefix="elephant auto '") as tmp:
    root = pathlib.Path(tmp)
    binary = root / "elephant ' binary"
    shutil.copy2(pathlib.Path(sys.argv[1]).resolve(), binary)
    project = root / "project ' $(touch injected)"
    project.mkdir()
    (project / "go.mod").write_text("module demo\ngo 1.22\n")
    store = root / "store" / "events.jsonl"

    def cli(command, *args, stdin=None, cwd=project):
        p = subprocess.run([str(binary), command, *args], input=stdin,
                           text=True, capture_output=True, cwd=cwd, check=True)
        assert not p.stderr, p.stderr
        return json.loads(p.stdout) if p.stdout.strip() else None

    result = cli("init", "--project", "pool-app", "--tenant", "pilot",
                 "--user", "tester", "--store", str(store))
    assert result["configured"] and len(result["agents"]) == 2
    assert cli("init")["changed_files"] == []
    hooks = {
        "codex": json.loads((project / ".codex/hooks.json").read_text())["hooks"],
        "claude": json.loads((project / ".claude/settings.local.json").read_text())["hooks"],
    }

    def hook(agent, event, **args):
        command = hooks[agent][event][0]["hooks"][0]["command"]
        payload = {"hook_event_name": event, "session_id": "session-1", "cwd": str(project), **args}
        p = subprocess.run(command, shell=True, executable="/bin/sh", cwd=project,
                           input=json.dumps(payload), text=True, capture_output=True, check=True)
        assert not p.stderr, p.stderr
        return json.loads(p.stdout)

    for agent in hooks:
        hook(agent, "SessionStart")
        hook(agent, "UserPromptSubmit", prompt="Repair the query connection pool")
        hook(agent, "PostToolUse", tool_name="Bash", tool_use_id="test-1",
             tool_input={"command": "SECRET_COMMAND"},
             tool_response={"exit_code": 1, "stdout": "SECRET_OUTPUT"},
             transcript_path="SECRET_TRANSCRIPT")
        review = hook(agent, "Stop")
        assert review["decision"] == "block"
        assert hook(agent, "Stop") == {}
        assert hook(agent, "Stop", stop_hook_active=True) == {}
        # The host agent supplies extraction; simulate its exact stdin command.
        command = review["reason"].split("this command:\n", 1)[1].split("\nJSON fields:", 1)[0]
        lesson = {"scope": "project", "class": "warning", "incident": "The pool test failed.",
                  "lesson": "Limit query concurrency to the connection pool size.",
                  "source": "Synthetic acceptance: bounded pool test passed after the fix.",
                  "features": {"language": ["go"]}}
        p = subprocess.run(shlex.split(command), input=json.dumps(lesson), text=True,
                           capture_output=True, check=True, cwd=project)
        memory = json.loads(p.stdout)
        status_command = review["reason"].split("output once in your final response:\n", 1)[1].split("\nDo not invent counts", 1)[0]
        status_args = shlex.split(status_command)
        incomplete = subprocess.run(status_args[:-1] + ["--json"], capture_output=True, text=True, check=True)
        assert json.loads(incomplete.stdout)["state"] == "review_incomplete"
        summary = subprocess.run(status_args, capture_output=True, text=True, check=True)
        assert summary.stdout.strip().startswith("Elephant: recalled ") and summary.stdout.strip().endswith(" · saved 1 lesson."), summary.stdout
        assert len(summary.stdout.splitlines()) == 1
        assert memory["project"] == "pool-app" and memory["owner"] == "tester"
        recalled = hook(agent, "UserPromptSubmit", prompt="Change query concurrency in the connection pool")
        context = recalled["hookSpecificOutput"]["additionalContext"]
        assert "Limit query concurrency" in context and len(context.encode()) <= 4000
        next_review = hook(agent, "Stop")
        assert next_review["decision"] == "block"
        status_args = shlex.split(next_review["reason"].split("output once in your final response:\n", 1)[1].split("\nDo not invent counts", 1)[0])
        summary = subprocess.run(status_args + ["--json"], capture_output=True, text=True, check=True)
        summary = json.loads(summary.stdout)
        assert summary["recalled"] == 1 and summary["saved"] == 0 and summary["state"] == "no_lesson"
        hook(agent, "UserPromptSubmit", prompt="Task with invalid lesson")
        failed_review = hook(agent, "Stop")
        failed_command = failed_review["reason"].split("this command:\n", 1)[1].split("\nJSON fields:", 1)[0]
        failed = subprocess.run(shlex.split(failed_command), input='{}', capture_output=True, text=True)
        assert failed.returncode != 0
        status_args = shlex.split(failed_review["reason"].split("output once in your final response:\n", 1)[1].split("\nDo not invent counts", 1)[0])
        failed_status = subprocess.run(status_args + ["--json"], capture_output=True, text=True, check=True)
        assert json.loads(failed_status.stdout)["state"] == "capture_failed"
    status = cli("automation")
    assert status["experiences"] > 0
    assert len(cli("status")["memories"]) == 1  # Independent identical saves deduplicate.
    subdir = project / "nested"
    subdir.mkdir()
    assert cli("automation", cwd=subdir)["config"]["project"] == "pool-app"
    assert cli("experiences", cwd=subdir)["total"] == status["experiences"]
    cli("automation", "--enabled=false")
    before = store.read_bytes()
    assert hook("codex", "UserPromptSubmit", prompt="Paused task") == {}
    assert hook("codex", "Stop") == {}
    assert before == store.read_bytes()
    cli("automation", "--enabled=true")
    journal = store.read_bytes().decode("utf-8", errors="replace")
    for secret in ("SECRET_COMMAND", "SECRET_OUTPUT", "SECRET_TRANSCRIPT", "Repair the query connection pool"):
        assert secret not in journal
    assert not (project / "injected").exists()
    # Failures must be visible without blocking the user's work or leaking input.
    p = subprocess.run([str(binary), "hook", "--agent", "codex", "--config", result["config"]],
                       input="broken", text=True, capture_output=True, cwd=project, check=True)
    assert "systemMessage" in json.loads(p.stdout) and "invalid hook input" in p.stderr
    print(json.dumps({"generated_hook_commands": True, "paths_with_quotes": True,
                      "automatic_event_capture": True, "one_review_per_task": True,
                      "agent_record_command": True, "recall_next_task": True,
                      "config_inheritance": True, "pause_resume": True,
                      "sensitive_payloads_omitted": True, "task_status_acknowledged_saves": True, "no_lesson_and_capture_failure": True,
                      "native_host_model_run": False}))

