#!/usr/bin/env python3
"""Measure synthetic local Elephant journeys through the built binary."""

import argparse
import hashlib
import json
import math
import os
import platform
import queue
import socket
import subprocess
import tempfile
import threading
import time
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
COMMAND_TIMEOUT = 60


def stop_process(process):
    if process.poll() is None:
        process.terminate()
    try:
        process.communicate(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.communicate()


def read_ready_line(stream):
    lines = queue.Queue()
    threading.Thread(target=lambda: lines.put(stream.readline()), daemon=True).start()
    try:
        return lines.get(timeout=COMMAND_TIMEOUT).strip()
    except queue.Empty:
        raise RuntimeError("Memory Palace startup timed out") from None


def percentile(values, percent):
    if not values:
        return 0.0
    ordered = sorted(values)
    return ordered[max(0, math.ceil(percent * len(ordered) / 100) - 1)]


def summary(values):
    return {
        "samples": len(values),
        "p50_ms": round(percentile(values, 50), 3),
        "p75_ms": round(percentile(values, 75), 3),
        "p95_ms": round(percentile(values, 95), 3),
        "min_ms": round(min(values), 3) if values else 0.0,
        "max_ms": round(max(values), 3) if values else 0.0,
    }


def run_json(command, *, data=None, cwd=ROOT):
    completed = subprocess.run(
        [str(x) for x in command], cwd=cwd, input=data, text=True,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True, timeout=COMMAND_TIMEOUT,
    )
    return json.loads(completed.stdout)


def seed_store(store, size):
    env = os.environ.copy()
    env["ELEPHANT_PERF_FIXTURE"] = str(store)
    env["ELEPHANT_PERF_SIZE"] = str(size)
    subprocess.run(
        ["go", "test", "-run", "^TestGeneratePerformanceFixture$", "-count=1", "."],
        cwd=ROOT, env=env, stdout=subprocess.DEVNULL, check=True, timeout=300,
    )


def hook_recall(binary, config, root, session):
    payload = json.dumps({
        "hook_event_name": "UserPromptSubmit",
        "session_id": session,
        "cwd": str(root),
        "prompt": "query concurrency",
    })
    start = time.perf_counter_ns()
    completed = subprocess.run(
        [str(binary), "hook", "--config", str(config), "--agent", "codex"],
        input=payload, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True,
        timeout=COMMAND_TIMEOUT,
    )
    output = json.loads(completed.stdout)
    elapsed = (time.perf_counter_ns() - start) / 1_000_000
    context = output.get("hookSpecificOutput", {}).get("additionalContext", "")
    if not context or len(context.encode("utf-8")) > 4000:
        raise RuntimeError("synthetic hook recall returned missing or oversized context")
    return elapsed


def trace_recall(binary, root, store, size):
    output = run_json([
        binary, "recall", "--trace", "--root", root, "--project", "perf",
        "--store", store, "--tenant", "local", "--user", "me",
        "--task", "query concurrency", "--budget", "4000",
    ])
    if output["trace"]["candidates"] != size or not output["result"]["hits"]:
        raise RuntimeError("recall trace did not retrieve the seeded fixture")
    return output["trace"]


def measure_recall(binary, work, size, samples):
    root = work / f"project-{size}"
    root.mkdir()
    root = root.resolve()
    (root / "go.mod").write_text("module example.test/perf\n")
    store = work / f"memory-{size}.sqlite"
    seed_store(store, size)
    config_dir = root / ".elephant"
    config_dir.mkdir()
    config = config_dir / "automation.json"
    config.write_text(json.dumps({
        "version": 1,
        "enabled": True,
        "root": str(root),
        "project": "perf",
        "store": str(store),
        "identity": {"tenant": "local", "user": "me", "team": ""},
        "binary": str(binary),
        "byte_budget": 4000,
        "agents": ["codex"],
    }))
    values = [hook_recall(binary, config, root, f"perf-{size}-{index}") for index in range(samples)]
    return {
        "fixture_memories": size,
        "first_process_ms": round(values[0], 3),
        "subsequent_processes": summary(values[1:]),
        "phase_trace_ms": trace_recall(binary, root, store, size),
    }


def measure_setup(binary, work):
    root = work / "setup-project"
    root.mkdir()
    root = root.resolve()
    (root / "go.mod").write_text("module example.test/setup\n")
    store = work / "setup.sqlite"
    journey_start = time.perf_counter_ns()
    start = journey_start
    run_json([
        binary, "init", "--root", root, "--project", "perf-setup", "--store", store,
        "--tenant", "local", "--user", "me", "--agent", "codex",
    ])
    init_ms = (time.perf_counter_ns() - start) / 1_000_000
    start = time.perf_counter_ns()
    mcp = subprocess.Popen(
        [str(binary), "mcp", "--root", str(root), "--project", "perf-setup", "--store", str(store),
         "--tenant", "local", "--user", "me"],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
    )
    requests = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-06-18"}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
    ]
    try:
        stdout, _ = mcp.communicate("".join(json.dumps(request) + "\n" for request in requests), timeout=COMMAND_TIMEOUT)
    finally:
        stop_process(mcp)
    discovery_ms = (time.perf_counter_ns() - start) / 1_000_000
    responses = {response.get("id"): response for response in map(json.loads, stdout.splitlines())}
    initialize, tools = responses.get(1, {}), responses.get(2, {})
    if mcp.returncode or "result" not in initialize or len(tools.get("result", {}).get("tools", [])) != 6:
        raise RuntimeError("MCP discovery did not return six tools")
    return {"init_ms": round(init_ms, 3), "mcp_discovery_ms": round(discovery_ms, 3),
            "total_ms": round((time.perf_counter_ns() - journey_start) / 1_000_000, 3), "tool_count": 6}


def measure_capture(binary, work, samples):
    root = work / "capture-project"
    root.mkdir()
    root = root.resolve()
    (root / "go.mod").write_text("module example.test/capture\n")
    store = work / "capture.sqlite"
    values = []
    ids = set()
    for index in range(samples):
        memory = json.dumps({
            "scope": "project", "class": "win",
            "incident": f"Synthetic capture journey {index}",
            "lesson": "Use bounded queries in the measured hot path.",
            "source": "SYNTHETIC PERFORMANCE FIXTURE",
            "features": {"language": ["go"]},
        })
        start = time.perf_counter_ns()
        receipt = run_json([
            binary, "remember", "--root", root, "--project", "perf-capture", "--store", store,
            "--tenant", "local", "--user", "me",
        ], data=memory)
        values.append((time.perf_counter_ns() - start) / 1_000_000)
        ids.add(receipt["id"])
    persisted = run_json([binary, "status", "--root", root, "--project", "perf-capture", "--store", store,
                          "--tenant", "local", "--user", "me"])
    if len(ids) != samples or ids != {memory["id"] for memory in persisted["memories"]}:
        raise RuntimeError("capture receipts did not survive reopening the store")
    return {**summary(values), "verified_memories_after_reopen": len(ids)}


def measure_palace(binary, work, store, size):
    with socket.socket() as candidate:
        candidate.bind(("127.0.0.1", 0))
        port = candidate.getsockname()[1]
    start = time.perf_counter_ns()
    process = subprocess.Popen(
        [str(binary), "palace", "--root", str(work), "--project", "perf", "--store", str(store), "--port", str(port),
         "--tenant", "local", "--user", "me"],
        stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True,
    )
    try:
        line = read_ready_line(process.stderr)
        ready_ms = (time.perf_counter_ns() - start) / 1_000_000
        if "#token=" not in line:
            raise RuntimeError("Memory Palace did not print an authenticated URL")
        token = line.split("#token=", 1)[1]
        request = urllib.request.Request(
            f"http://127.0.0.1:{port}/api/dashboard",
            headers={"Authorization": f"Bearer {token}"},
        )
        api_start = time.perf_counter_ns()
        with urllib.request.urlopen(request, timeout=COMMAND_TIMEOUT) as response:
            payload = response.read()
        api_ms = (time.perf_counter_ns() - api_start) / 1_000_000
        total_ms = (time.perf_counter_ns() - start) / 1_000_000
        dashboard = json.loads(payload)
        if len(dashboard["memories"]) != size or dashboard["identity"]["user"] != "me":
            raise RuntimeError("Memory Palace did not return the seeded fixture")
        return {"ready_ms": round(ready_ms, 3), "dashboard_api_ms": round(api_ms, 3),
                "total_ms": round(total_ms, 3), "payload_bytes": len(payload), "verified_memories": size}
    finally:
        stop_process(process)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path)
    parser.add_argument("--sizes", default="100,1000,10000")
    parser.add_argument("--samples", type=int, default=10)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    try:
        sizes = [int(value) for value in args.sizes.split(",")]
    except ValueError:
        parser.error("sizes must be comma-separated integers")
    if args.samples < 3 or len(set(sizes)) != len(sizes) or any(size < 1 or size > 100000 for size in sizes):
        raise SystemExit("samples must be >=3 and sizes must be 1..100000")
    # The lab must never fetch updates or inherit user identity defaults.
    os.environ["ELEPHANT_UPDATE_CHECKS"] = "0"
    with tempfile.TemporaryDirectory(prefix="elephant-perf-") as directory:
        work = Path(directory)
        binary = args.binary.resolve() if args.binary else work / "elephant-perf"
        if args.binary is None:
            subprocess.run(["go", "build", "-buildvcs=false", "-o", binary, "./cmd/elephant"], cwd=ROOT, check=True)
        recalls = [measure_recall(binary, work, size, args.samples) for size in sizes]
        first_store = work / f"memory-{sizes[0]}.sqlite"
        report = {
            "version": 1,
            "synthetic": True,
            "revision": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip(),
            "working_tree_dirty": bool(subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=no"], cwd=ROOT, text=True).strip()),
            "cache_note": "Each sample launches a fresh process. OS caches are uncontrolled; fixtures were just seeded.",
            "environment": {
                "os": platform.platform(), "machine": platform.machine(),
                "python": platform.python_version(),
                "go": subprocess.check_output(["go", "version"], text=True).strip(),
                "binary_sha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
            },
            "journeys": {
                "setup_to_six_tools": measure_setup(binary, work),
                "task_to_recall": recalls,
                "capture_to_durable_receipt": measure_capture(binary, work, args.samples),
                "palace_to_dashboard": measure_palace(binary, work / f"project-{sizes[0]}", first_store, sizes[0]),
            },
            "privacy": "Synthetic content only. Task text and Memory content are not emitted in this report.",
        }
        text = json.dumps(report, indent=2, sort_keys=True) + "\n"
        if args.output:
            args.output.write_text(text)
        else:
            print(text, end="")


if __name__ == "__main__":
    main()
