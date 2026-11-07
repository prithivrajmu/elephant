"""Exercise detached idle scheduling with a synthetic local reviewer; no model calls."""
import datetime
import json
import pathlib
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time

binary = str(pathlib.Path(sys.argv[1]).resolve())

with tempfile.TemporaryDirectory(prefix="elephant-idle-") as tmp:
    root = pathlib.Path(tmp).resolve()
    # Go exec resolves Windows executables through PATHEXT; match release names.
    if sys.platform == "win32" and pathlib.Path(binary).suffix.lower() != ".exe":
        packaged_binary = root / "elephant-test.exe"
        source_binary = pathlib.Path(binary)
        if not source_binary.is_file():
            source_binary = pathlib.Path(binary + ".exe")
        shutil.copyfile(source_binary, packaged_binary)
        binary = str(packaged_binary)
    store = root / "memory.sqlite"
    config_path = root / ".elephant" / "automation.json"
    config_path.parent.mkdir()
    config_path.write_text(json.dumps({
        "version": 1, "enabled": True, "root": str(root), "project": "idle-acceptance",
        "store": str(store), "identity": {"tenant": "test", "user": "alice"},
        "binary": binary, "byte_budget": 4000, "agents": ["codex"],
    }))
    marker = root / "reviewer-called"
    reviewer = root / "reviewer.py"
    reviewer.write_text(
        "import json,os,pathlib,sys\n"
        "request=json.load(sys.stdin)\n"
        "assert os.environ['ELEPHANT_DREAM_RUN']=='1'\n"
        "assert len(request['batch']['candidates'])==1\n"
        "assert request['output_schema']['properties']['replacements']['maxItems']==3\n"
        "pathlib.Path(sys.argv[1]).write_text('called')\n"
        "print(json.dumps({'replacements':[]}))\n"
    )
    runner_file = root / "runner.json"
    runner_file.write_text(json.dumps([sys.executable, str(reviewer), str(marker)]))

    def cli(command, *extra, body=None):
        result = subprocess.run([binary, command, "--root", str(root), *extra],
                                input=json.dumps(body) if body is not None else None,
                                text=True, capture_output=True, timeout=15)
        assert result.returncode == 0, result.stderr
        return json.loads(result.stdout) if result.stdout.strip() else None

    def wait_status(predicate):
        deadline = time.monotonic() + 12
        while time.monotonic() < deadline:
            status = cli("dream-schedule")
            if predicate(status):
                return status
            time.sleep(0.1)
        raise AssertionError(f"idle watcher did not reach expected state: {status}")

    try:
        cli("remember", body={"class": "lesson", "incident": "SYNTHETIC idle review",
                              "lesson": "Check the pool limit.", "source": "SYNTHETIC test"})
        cli("dream-schedule", "--enabled=true", "--runner-command", str(runner_file))
        wait_status(lambda s: s["watcher_running"])
        assert not marker.exists(), "configuration alone launched a model review"
        for event in ["UserPromptSubmit", "Stop"]:
            cli("hook", "--config", str(config_path), "--agent", "codex", body={
                "hook_event_name": event, "session_id": "acceptance-session",
                "cwd": str(root), "prompt": "Inspect the pool limit",
            })
        # Advance persisted activity time instead of making CI wait ten minutes.
        with sqlite3.connect(store, timeout=10) as db:
            db.execute("BEGIN IMMEDIATE")
            key, data = db.execute("SELECT context,data FROM dream_idle").fetchone()
            state = json.loads(data)
            assert not state["active_sessions"]
            state["last_activity"] = (datetime.datetime.now(datetime.timezone.utc)
                                      - datetime.timedelta(minutes=11)).isoformat()
            db.execute("UPDATE dream_idle SET data=? WHERE context=?", (json.dumps(state), key))
        status = wait_status(lambda s: s["state"].get("last_result") == "no_change")
        assert marker.exists() and status["state"]["attempts_today"] == 1
        assert status["state"]["reviewed_generation"] == status["state"]["generation"]
        assert not status["state"].get("last_saved_ids")
    finally:
        cli("dream-schedule", "--enabled=false")
        wait_status(lambda s: not s["watcher_running"])

print("Detached idle watcher, hook activity, bounded reviewer protocol, no-change receipt and pause passed.")
