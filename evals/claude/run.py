#!/usr/bin/env python3
"""A/B eval of Elephant in Claude Code: each task runs with and without Elephant.

  python3 evals/claude/run.py --reps 3 --model sonnet --effort medium

Both arms get the same fixture, tools and settings (--setting-sources project,local
keeps user hooks out). Only the with-arm gets `elephant init` hooks and seeded lessons.
Each run uses a fresh temp dir and store, so captured lessons never leak between runs.
Cost, tokens and turns come from `claude -p --output-format json`.
"""
import argparse, json, os, pathlib, shutil, statistics, subprocess, tempfile, time

HERE = pathlib.Path(__file__).parent
ELEPHANT = os.environ.get("ELEPHANT", "elephant")
PROJECT = "evalproj"
TOOLS = ["Bash", "Read", "Edit", "Write", "Glob", "Grep"]


def elephant(*args, check=True):
    r = subprocess.run([ELEPHANT, *args], capture_output=True, text=True)
    assert not check or r.returncode == 0, f"elephant {args[0]} failed: {r.stderr or r.stdout}"
    return r.stdout


# Arms isolate where Elephant's overhead comes from. Differences between neighbours:
#   empty-nostop - without : CLAUDE.md guidance + hook plumbing
#   empty - empty-nostop   : the end-of-task review turn(s)
#   recall-only - empty-nostop : injected lessons
ARMS = {"without": None,
        "empty": dict(seed=False, stop=True),
        "empty-nostop": dict(seed=False, stop=False),
        "recall-only": dict(seed=True, stop=False),
        "with": dict(seed=True, stop=True)}
LESSONS = sorted((HERE / "lessons").glob("*.json"))


def setup(root, store, seeded, stop):
    common = ["--root", str(root), "--project", PROJECT, "--store", str(store), "--tenant", "eval", "--user", "eval"]
    elephant("init", "--agent", "claude", *common)
    if seeded:
        for f in LESSONS:
            elephant("remember", *common, "--file", str(f))
    if not stop:
        path = pathlib.Path(root) / ".claude" / "settings.local.json"
        cfg = json.loads(path.read_text())
        cfg["hooks"].pop("Stop")
        path.write_text(json.dumps(cfg, indent=2))
    return common


def one(task, arm, rep, model, effort, tmp):
    spec = json.load(open(HERE / "tasks" / task / "task.json"))
    run = pathlib.Path(tmp) / f"{task}-{arm}-{rep}"
    shutil.copytree(HERE / "tasks" / task / "repo", run)
    store = pathlib.Path(tmp) / f"{task}-{arm}-{rep}.sqlite"
    if ARMS[arm]:
        common = setup(run, store, ARMS[arm]["seed"], ARMS[arm]["stop"])
    cmd = ["claude", "-p", "--output-format", "json", "--model", model, "--effort", effort, "--no-session-persistence",
           "--setting-sources", "project,local", "--permission-mode", "acceptEdits",
           "--allowedTools", *TOOLS, "--max-budget-usd", "2", spec["prompt"]]
    env = {**os.environ, "TZ": "Asia/Kolkata"}  # the tz-tests trap needs a non-UTC host
    t0 = time.time()
    r = subprocess.run(cmd, cwd=run, env=env, capture_output=True, text=True, timeout=900)
    try:
        d = json.loads(r.stdout)
    except ValueError:
        d = {"is_error": True, "result": (r.stderr or r.stdout)[:300]}
    u = d.get("usage", {})
    row = {"task": task, "category": spec["category"], "arm": arm, "rep": rep, "model": model, "effort": effort,
           "cost_usd": d.get("total_cost_usd"), "turns": d.get("num_turns"), "error": d.get("is_error", False),
           "output_tokens": u.get("output_tokens"), "input_tokens": u.get("input_tokens"),
           "cache_read": u.get("cache_read_input_tokens"), "cache_write": u.get("cache_creation_input_tokens"),
           "seconds": round(time.time() - t0, 1)}
    row["pass"] = subprocess.run(["bash", str(HERE / "tasks" / task / "check.sh")], cwd=run, capture_output=True,
                                 env={**os.environ, "TASK_DIR": str(HERE / "tasks" / task)}).returncode == 0
    if ARMS[arm]:
        st = json.loads(elephant("status", *common, check=False) or "{}")
        row["recalled_ids"] = sorted({i for x in st.get("usage", []) for i in x.get("memory_ids", [])})
        subject = spec["lesson"] and json.load(open(HERE / "lessons" / f"{spec['lesson']}.json"))["subject"]
        hit = bool(subject) and any(m.get("subject") == subject and m["id"] in row["recalled_ids"] for m in st.get("memories", []))
        row["lesson_recalled"] = hit if subject else None
        row["irrelevant_recalled"] = len(row["recalled_ids"]) - int(hit)
        row["injected_tokens"] = st.get("injected_tokens")
        row["memories_saved"] = max(0, len([m for m in st.get("memories", []) if not m.get("retired")]) - (len(LESSONS) if ARMS[arm]["seed"] else 0))
    return row


def med(rows, k):
    v = [r[k] for r in rows if r[k] is not None]
    return statistics.median(v) if v else float("nan")


def summarize(rows):
    arms = [a for a in ARMS if any(r["arm"] == a for r in rows)]
    print(f"\n{'task':<18}{'arm':<13}{'n':>3}{'pass':>6}{'cost$':>9}{'turns':>7}{'out_tok':>9}{'inj_tok':>9}{'sec':>6}")
    for task in dict.fromkeys(r["task"] for r in rows):
        for arm in arms:
            g = [r for r in rows if r["task"] == task and r["arm"] == arm]
            if g:
                print(f"{task:<18}{arm:<13}{len(g):>3}{sum(r['pass'] for r in g):>4}/{len(g):<2}"
                      f"{med(g, 'cost_usd'):>9.4f}{med(g, 'turns'):>7.1f}{med(g, 'output_tokens'):>9.0f}"
                      f"{med(g, 'injected_tokens') if arm != 'without' else 0:>9.0f}{med(g, 'seconds'):>6.0f}")
    w = [r for r in rows if r["arm"] in ARMS and ARMS[r["arm"]] and ARMS[r["arm"]]["seed"]]
    if w:
        rel = [r for r in w if r["lesson_recalled"] is not None]
        print(f"\nseeded arms recall: right lesson surfaced {sum(r['lesson_recalled'] for r in rel)}/{len(rel)}; "
              f"irrelevant lessons injected per run, median {statistics.median(r['irrelevant_recalled'] for r in w):.0f}; "
              f"lessons saved per run, median {statistics.median(r['memories_saved'] for r in w):.0f}")
    for arm in arms:
        g = [r for r in rows if r["arm"] == arm]
        if g:
            print(f"TOTAL {arm:<13} pass {sum(r['pass'] for r in g)}/{len(g)}  cost ${sum(r['cost_usd'] or 0 for r in g):.3f}")
    print("Small n: treat deltas as signal, not proof. Check the 'control' row for pure overhead.")


if __name__ == "__main__":
    p = argparse.ArgumentParser()
    p.add_argument("--reps", type=int, default=3)
    p.add_argument("--model", default="sonnet")
    p.add_argument("--effort", default="medium")
    p.add_argument("--tasks", default="")
    p.add_argument("--arms", default="without,with", help="comma list from: " + ",".join(ARMS))
    p.add_argument("--out", default="")
    a = p.parse_args()
    tasks = a.tasks.split(",") if a.tasks else sorted(d.name for d in (HERE / "tasks").iterdir() if d.is_dir())
    out = pathlib.Path(a.out or HERE / "results" / time.strftime("%Y%m%d-%H%M%S.jsonl"))
    out.parent.mkdir(exist_ok=True)
    rows = []
    with tempfile.TemporaryDirectory() as tmp, open(out, "w") as f:
        for rep in range(a.reps):
            for task in tasks:
                for arm in a.arms.split(","):  # interleaved so drift hits both arms
                    row = one(task, arm, rep, a.model, a.effort, tmp)
                    rows.append(row)
                    f.write(json.dumps(row) + "\n"); f.flush()
                    print(f"{task} {arm} #{rep}: pass={row['pass']} ${row['cost_usd']} turns={row['turns']}", flush=True)
    summarize(rows)
    print("results:", out)
