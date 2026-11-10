#!/usr/bin/env python3
"""Validate an explicitly supplied local experiment manifest. Never runs a model."""
import argparse
import hashlib
import json
from pathlib import Path
import re


def load_json(path):
    raw = path.read_bytes()
    if len(raw) > 1024*1024:
        raise ValueError("manifest exceeds 1 MiB")
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate JSON key")
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=unique)


def validate(manifest, root):
    root = Path(root).resolve()
    if manifest.get("schema_version") != 1:
        raise ValueError("unsupported manifest schema")
    stage = manifest.get("stage")
    if stage not in ("development", "pilot", "confirmatory"):
        raise ValueError("unknown experiment stage")
    cases = manifest.get("cases")
    if not isinstance(cases, list) or not 1 <= len(cases) <= 1000:
        raise ValueError("require 1..1000 cases")
    source_ids, fingerprints, groups, case_ids, splits = {}, {}, {}, set(), set()
    total_bytes = 0
    for case in cases:
        name, group, split = (case.get(k) for k in ("id", "incident_family", "split"))
        if not all(isinstance(x, str) and x.strip() for x in (name, group, split)) or name in case_ids:
            raise ValueError("invalid or duplicate case identity")
        if split not in ("development", "pilot", "holdout"):
            raise ValueError("unknown case split")
        case_ids.add(name); splits.add(split)
        if group in groups and groups[group] != split:
            raise ValueError("incident family crosses splits")
        groups[group] = split
        sources = case.get("sources")
        if not isinstance(sources, list) or not 1 <= len(sources) <= 20:
            raise ValueError("require 1..20 explicit sources per case")
        for source in sources:
            sid, relative, digest = (source.get(k) for k in ("id", "path", "sha256"))
            if not isinstance(sid, str) or not sid.strip() or not isinstance(relative, str) or not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
                raise ValueError("invalid source identity")
            if Path(relative).is_absolute():
                raise ValueError("source paths must be relative to the manifest")
            path = (root/relative).resolve()
            if not path.is_relative_to(root) or not path.is_file():
                raise ValueError("source is missing or outside manifest root")
            with path.open("rb") as stream:
                content = stream.read(1024*1024+1)
            total_bytes += len(content)
            if len(content)>1024*1024 or total_bytes>16*1024*1024:
                raise ValueError("source byte budget exceeded")
            if hashlib.sha256(content).hexdigest() != digest:
                raise ValueError("source content hash changed")
            start, end, span_hash = (source.get(k) for k in ("start_byte", "end_byte", "span_sha256"))
            if type(start) is not int or type(end) is not int or not 0 <= start < end <= len(content):
                raise ValueError("invalid byte span")
            if hashlib.sha256(content[start:end]).hexdigest() != span_hash:
                raise ValueError("source span hash changed")
            identity = (digest, relative)
            if sid in source_ids and source_ids[sid] != identity:
                raise ValueError("source ID reused with different content or path")
            source_ids[sid] = identity
            if digest in fingerprints and fingerprints[digest] != split:
                raise ValueError("identical source content crosses splits")
            fingerprints[digest] = split
    configured = stage != "development"
    if configured:
        design = manifest.get("design", {})
        required = ("reader_model", "reviewer_model", "primary_endpoint", "primary_comparison", "analysis", "stopping_rule", "resource_budget")
        if any(not isinstance(design.get(k), str) or not design[k].strip() or design[k].strip().lower() in ("tbd", "unknown", "not_selected") for k in required):
            raise ValueError("experiment design is incomplete")
        if not re.fullmatch(r"[0-9a-f]{40}", design.get("git_commit", "")):
            raise ValueError("require exact code commit")
        if not isinstance(design.get("seeds"), list) or not design["seeds"] or any(type(x) is not int for x in design["seeds"]):
            raise ValueError("require explicit integer seeds")
        if stage == "confirmatory" and "holdout" not in splits:
            raise ValueError("confirmatory runs require holdout cases")
        if stage == "pilot" and "holdout" in splits:
            raise ValueError("pilot must not consume the sealed holdout")
    canonical = json.dumps(manifest, sort_keys=True, separators=(",", ":")).encode()
    return {"schema_version": 1, "stage": stage,
            "manifest_sha256": hashlib.sha256(canonical).hexdigest(),
            "cases": len(case_ids), "declared_incident_families": len(groups),
            "distinct_source_hashes": len(fingerprints),
            "design_fields_present": configured, "preflight_passed": True,
            "efficacy_established": False,
            "limitations": ["Hashes establish identity, not source truth or authority.",
                            "Declared incident families do not establish semantic independence.",
                            "Passing preflight does not establish adequate power or preregistration."]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    args = parser.parse_args()
    try:
        result = validate(load_json(args.manifest), args.manifest.parent)
    except (ValueError, OSError, TypeError, KeyError, AttributeError):
        parser.exit(1, "Preflight failed: invalid design, source identity/span, path, or split isolation.\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
