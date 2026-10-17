import json
import sys


def render():
    schema = json.load(open("schema.json"))
    out = ["from dataclasses import dataclass", ""]
    for name, fields in schema.items():
        out += ["", "@dataclass", f"class {name}:"]
        out += [f"    {k}: {v}" for k, v in fields.items()]
    return "\n".join(out) + "\n"


if "--check" in sys.argv:
    sys.exit(0 if open("models_gen.py").read() == render() else 1)
open("models_gen.py", "w").write(render())
