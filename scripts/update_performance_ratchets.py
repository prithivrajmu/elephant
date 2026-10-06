#!/usr/bin/env python3
"""Check deterministic performance ceilings and optionally ratchet them down."""

import argparse
import json
import os
import subprocess
import tempfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
RATCHETS = ROOT / "testdata" / "performance-ratchets.json"


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--lower", action="store_true", help="lower ceilings to current measurements")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="elephant-ratchets-") as directory:
        metrics_path = Path(directory) / "metrics.json"
        env = os.environ.copy()
        env["ELEPHANT_PERF_METRICS"] = str(metrics_path)
        run = subprocess.run(
            ["go", "test", "-run", "^TestPerformanceRatchets$", "-count=1", "."],
            cwd=ROOT,
            env=env,
            check=False,
        )
        if not metrics_path.exists():
            return run.returncode or 1
        measured = json.loads(metrics_path.read_text())["metrics"]
        ratchets = json.loads(RATCHETS.read_text())
        ceilings = ratchets["ceilings"]
        regressions = {
            key: (value, ceilings[key])
            for key, value in measured.items()
            if key in ceilings and value > ceilings[key]
        }
        if regressions:
            for key, (value, ceiling) in sorted(regressions.items()):
                print(f"{key}: measured {value} exceeds ceiling {ceiling}")
            return 1
        if args.lower:
            changed = False
            for key, value in measured.items():
                if key not in ceilings:
                    raise SystemExit(f"measured metric has no ceiling: {key}")
                if value < ceilings[key]:
                    ceilings[key] = value
                    changed = True
            if changed:
                RATCHETS.write_text(json.dumps(ratchets, indent=2, sort_keys=True) + "\n")
                print(f"lowered ceilings in {RATCHETS.relative_to(ROOT)}")
            else:
                print("no ceilings can be lowered")
        else:
            print(json.dumps({"version": 1, "metrics": measured, "ceilings": ceilings}, indent=2, sort_keys=True))
        return run.returncode


if __name__ == "__main__":
    raise SystemExit(main())
