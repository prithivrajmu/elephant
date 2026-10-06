#!/usr/bin/env python3
"""Measure synthetic local Elephant journeys through the built binary."""

import argparse
import hashlib
import json
import math
import os
import platform
import socket
import subprocess
import tempfile
import time
import urllib.request
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


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
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True,
    )
    return json.loads(completed.stdout)


def seed_store(store, size):
    env = os.environ.copy()
    env["ELEPHANT_PERF_FIXTURE"] = str(store)
    env["ELEPHANT_PERF_SIZE"] = str(size)
    subprocess.run(
        ["go", "test", "-run", "^TestGeneratePerformanceFixture$", "-count=1", "."],
        cwd=ROOT, env=env, stdout=subprocess.DEVNULL, check=True,
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
    )
    output = json.loads(completed.stdout)
    elapsed = (time.perf_counter_ns() - start) / 1_000_000
    if "hookSpecificOutput" not in output:
        raise RuntimeError(f"synthetic hook recall returned no context: {output}; stderr={completed.stderr.strip()}")
    return elapsed


def trace_recall(binary, root, store):
    return run_json([
        binary, "recall", "--trace", "--root", root, "--project", "perf",
        "--store", store, "--tenant", "local", "--user", "me",
        "--task", "query concurrency", "--budget", "4000",
    ])["trace"]


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
        "cold_ms": round(values[0], 3),
        "warm": summary(values[1:]),
        "phase_trace_ms": trace_recall(binary, root, store),
    }


def measure_setup(binary, work):
    root = work / "setup-project"
    root.mkdir()
    root = root.resolve()
    (root / "go.mod").write_text("module example.test/setup\n")
    store = work / "setup.sqlite"
    start = time.perf_counter_ns()
    run_json([
        binary, "init", "--root", root, "--project", "perf-setup", "--store", store,
        "--tenant", "local", "--user", "me", "--agent", "codex",
    ])
    init_ms = (time.perf_counter_ns() - start) / 1_000_000
    mcp = subprocess.Popen(
        [str(binary), "mcp", "--root", str(root), "--project", "perf-setup", "--store", str(store)],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
    )
    start = time.perf_counter_ns()
    requests = [
        {"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {"protocolVersion": "2025-06-18"}},
        {"jsonrpc": "2.0", "method": "notifications/initialized"},
        {"jsonrpc": "2.0", "id": 2, "method": "tools/list"},
    ]
    for request in requests:
        mcp.stdin.write(json.dumps(request) + "\n")
    mcp.stdin.flush()
    initialize = json.loads(mcp.stdout.readline())
    tools = json.loads(mcp.stdout.readline())
    discovery_ms = (time.perf_counter_ns() - start) / 1_000_000
    mcp.stdin.close()
    mcp.wait(timeout=5)
    if "result" not in initialize or len(tools.get("result", {}).get("tools", [])) != 6:
        raise RuntimeError("MCP discovery did not return six tools")
    return {"init_ms": round(init_ms, 3), "mcp_discovery_ms": round(discovery_ms, 3), "tool_count": 6}


def measure_capture(binary, work, samples):
    root = work / "capture-project"
    root.mkdir()
    root = root.resolve()
    (root / "go.mod").write_text("module example.test/capture\n")
    store = work / "capture.sqlite"
    values = []
    for index in range(samples):
        memory = json.dumps({
            "scope": "project", "class": "win",
            "incident": f"Synthetic capture journey {index}",
            "lesson": "Use bounded queries in the measured hot path.",
            "source": "SYNTHETIC PERFORMANCE FIXTURE",
            "features": {"language": ["go"]},
        })
        start = time.perf_counter_ns()
        run_json([
            binary, "remember", "--root", root, "--project", "perf-capture", "--store", store,
            "--tenant", "local", "--user", "me",
        ], data=memory)
        values.append((time.perf_counter_ns() - start) / 1_000_000)
    return summary(values)


def measure_palace(binary, work, store):
    with socket.socket() as candidate:
        candidate.bind(("127.0.0.1", 0))
        port = candidate.getsockname()[1]
    start = time.perf_counter_ns()
    process = subprocess.Popen(
        [str(binary), "palace", "--root", str(work), "--project", "perf", "--store", str(store), "--port", str(port)],
        stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, text=True,
    )
    line = process.stderr.readline().strip()
    ready_ms = (time.perf_counter_ns() - start) / 1_000_000
    if "#token=" not in line:
        process.kill()
        raise RuntimeError("Memory Palace did not print an authenticated URL")
    token = line.split("#token=", 1)[1]
    request = urllib.request.Request(
        f"http://127.0.0.1:{port}/api/dashboard",
        headers={"Authorization": f"Bearer {token}"},
    )
    api_start = time.perf_counter_ns()
    with urllib.request.urlopen(request, timeout=5) as response:
        payload = response.read()
    api_ms = (time.perf_counter_ns() - api_start) / 1_000_000
    process.terminate()
    process.wait(timeout=5)
    return {"ready_ms": round(ready_ms, 3), "dashboard_api_ms": round(api_ms, 3), "payload_bytes": len(payload)}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path)
    parser.add_argument("--sizes", default="100,1000,10000")
    parser.add_argument("--samples", type=int, default=10)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    sizes = [int(value) for value in args.sizes.split(",")]
    if args.samples < 3 or any(size < 1 or size > 100000 for size in sizes):
        raise SystemExit("samples must be >=3 and sizes must be 1..100000")
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
                "palace_to_dashboard": measure_palace(binary, work, first_store),
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
