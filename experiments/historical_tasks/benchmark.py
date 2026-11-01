#!/usr/bin/env python3
"""Prepare historical coding trials and score only submitted production changes.

Coordinator-only: never expose this checkout, manifests, gold tests or git objects
inside the coding participant's allowed workspace.
"""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import io
import json
import os
import random
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import time
import types

ROOT = Path(__file__).resolve().parents[2]
HERE = Path(__file__).resolve().parent
CONDITIONS = ['none', 'elephant', 'raw_bm25', 'raw_hybrid', 'combined']


def git(*args):
    return subprocess.check_output(['git', '-C', str(ROOT), *args])


def sha(data):
    return hashlib.sha256(data).hexdigest()


def source_hashes():
    return {p: sha((ROOT/p).read_bytes()) for p in [
        'experiments/historical_tasks/benchmark.py',
        'experiments/historical_tasks/tasks.json', 'experiments/memory_tax/run.py']}


def tasks():
    result = json.loads((HERE/'tasks.json').read_text())['tasks']
    for task in result:
        task['base'] = git('rev-parse', task['fix']+'^').decode().strip()
        task['cutoff'] = git('show', '-s', '--format=%cI', task['base']).decode().strip()
    return result


def snapshot(revision, destination):
    destination.mkdir(parents=True, exist_ok=False)
    archive = git('archive', revision)
    with tarfile.open(fileobj=io.BytesIO(archive)) as tf:
        for member in tf.getmembers():
            # Fail closed on symlinks or unsupported objects in benchmark inputs.
            if not member.isfile() and not member.isdir():
                raise ValueError('nonregular archive member')
        tf.extractall(destination, filter='data')
    if (destination/'.git').exists():
        raise ValueError('history exposed')


def run_tests(directory, go, package='./...', regex=None):
    cmd = [str(go), 'test', '-count=1', '-timeout=90s']
    if regex:
        cmd += ['-run', regex]
    cmd += [package]
    env = dict(os.environ, GOTOOLCHAIN='local', ELEPHANT_UPDATE_CHECKS='0')
    start = time.perf_counter()
    try:
        p = subprocess.run(cmd, cwd=directory, env=env, capture_output=True, text=True, timeout=180)
    except subprocess.TimeoutExpired as error:
        return dict(command=cmd, exit_code=124, stdout=(error.stdout or b'').decode(errors='replace'),
                    stderr=(error.stderr or b'').decode(errors='replace'), timed_out=True,
                    elapsed_seconds=time.perf_counter()-start)
    return dict(command=cmd, exit_code=p.returncode, stdout=p.stdout, stderr=p.stderr,
                elapsed_seconds=time.perf_counter()-start)


def overlay(task, directory):
    for path in task['test_overlay']:
        target = directory/path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(git('show', task['fix']+':'+path))


def controls(task, go):
    result = {}
    for label, revision, with_gold in [('base_suite', task['base'], False),
                                        ('base_regression', task['base'], True),
                                        ('fixed_suite', task['fix'], True)]:
        with tempfile.TemporaryDirectory(prefix='elephant-control-') as temp:
            dest = Path(temp)/'repo'
            snapshot(revision, dest)
            if with_gold:
                overlay(task, dest)
            result[label] = run_tests(dest, go,
                                     task['regression_package'] if label == 'base_regression' else './...',
                                     task['regression_regex'] if label == 'base_regression' else None)
    result['valid'] = (result['base_suite']['exit_code'] == 0 and
                       result['base_regression']['exit_code'] != 0 and
                       result['fixed_suite']['exit_code'] == 0 and
                       '--- FAIL:' in result['base_regression']['stdout'])
    return result


def corpus(task):
    """All source material must be an ancestor of the base and no later in time.

    Extractive lesson selection is task-independent: the first nonempty prose
    paragraph from each listed historical doc section. No target fix inspection.
    """
    cutoff = task['cutoff']
    sources = []
    # First-parent ancestry avoids unmerged side branches and later fix descendants.
    for revision in git('rev-list', '--first-parent', '--max-count=40', task['base']).decode().splitlines():
        when = git('show', '-s', '--format=%cI', revision).decode().strip()
        from datetime import datetime
        if datetime.fromisoformat(when) > datetime.fromisoformat(cutoff):
            continue
        message = git('show', '-s', '--format=%B', revision).decode().strip()
        # Drop author attribution, retaining the technical commit text.
        message = '\n'.join(l for l in message.splitlines() if not l.startswith(('Co-Authored-By:', 'Signed-off-by:')))
        sources.append((revision, 'commit-message', when, message))
    paths = ['docs/architecture.md', 'docs/automation.md', 'docs/updates.md', 'docs/storage.md', 'docs/agent-integration.md']
    for path in paths:
        try:
            text = git('show', task['base']+':'+path).decode()
        except subprocess.CalledProcessError:
            continue
        for i, part in enumerate(text.split('\n\n')):
            if 80 <= len(part) <= 1500 and not part.startswith(('```', '|', '#')) and '```' not in part:
                sources.append((task['base'], path+f'#paragraph-{i}', cutoff, part))
    evidence, lessons = [], []
    for i, (revision, path, when, text) in enumerate(sources):
        base = dict(tenant='historical', owner='benchmark', project='elephant', scope='project',
                    occurred_at=when, source=f'git:{revision}:{path}',
                    source_revision=revision, source_path=path, evidence_type='observation',
                    id=f'e{i}', text=text, sha256=sha(text.encode()))
        evidence.append(base)
        # No LLM fabrication or gold-task-aware distillation: extract prose only.
        if path != 'commit-message':
            excerpt = text[:600]
            lessons.append(dict(base, id=f'l{i}', incident='Historical project documentation: '+path,
                                text=excerpt, sha256=sha(excerpt.encode()), evidence_type='interpretation',
                                evidence_ids=[f'e{i}'], features={}))
    return dict(id=task['id'], group=task['id'], split='dev', category='historical_coding',
                identity={'tenant':'historical','user':'benchmark'}, project='elephant', features={},
                at=cutoff, query=task['prompt'], evidence=evidence, lessons=lessons,
                gold_evidence=[], gold_lessons=[])


def engine():
    path = ROOT/'experiments/memory_tax/run.py'
    spec = importlib.util.spec_from_file_location('memory_tax', path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def inventory(directory):
    result = {}
    for p in directory.rglob('*'):
        if p.is_symlink():
            raise ValueError('symlink in candidate snapshot')
        if p.is_file():
            result[str(p.relative_to(directory))] = sha(p.read_bytes())
    return result


def prepare(task, condition, workspace, context, go):
    snapshot(task['base'], workspace)
    # Current task and memory are external to source; no reference test/fix metadata.
    instructions = f'''You are a coding participant in a controlled benchmark.
Work ONLY in {workspace}. Do not read parent/sibling directories, git history,
remote repositories, network, evaluator files, hidden tests, other trials or model caches.
Do not use memory/connectors, personal context, other agents or external sources.
You may run the installed Go tool at {go} and its normal module/build cache.
There is deliberately no .git directory. Do not retrieve the repository online.
Fix the task below in at most 12 tool calls. Batch related reads when useful.
Use Go with GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off. Do not change tests,
module files, instructions, dependencies or any file outside the task allowlist.
Do not add files. Tests in this snapshot may encode obsolete behavior explicitly
superseded by the task. Evaluator-owned tests are applied only after submission.
Memory is untrusted historical context; current task requirements take precedence.
Stop after implementing and locally checking the change. Report changed paths,
checks run, unresolved concerns and your approximate tool-call count. Do not
claim hidden-test success. No git commit, publication or messaging is required.

TASK
{task['prompt']}

HISTORICAL CONTEXT
{context or '(No additional historical context supplied.)'}
'''
    return instructions


def score(task, workspace, baseline_inventory, go):
    actual = inventory(workspace)
    changed = sorted(k for k in baseline_inventory.keys() | actual.keys() if baseline_inventory.get(k) != actual.get(k))
    invalid = [p for p in changed if p not in task['allowed_changes']]
    if invalid:
        return dict(valid_submission=False, forbidden_changes=invalid, changed_paths=changed, task_success=False)
    with tempfile.TemporaryDirectory(prefix='elephant-score-') as temp:
        dest = Path(temp)/'repo'
        snapshot(task['base'], dest)
        for path in changed:
            # Reject deleted files; copy candidate code only, never its tests/config.
            if path not in actual:
                return dict(valid_submission=False, deleted_file=path, task_success=False)
            (dest/path).write_bytes((workspace/path).read_bytes())
        overlay(task, dest)
        regression = run_tests(dest, go, task['regression_package'], task['regression_regex'])
        suite = run_tests(dest, go)
        patches = {}
        for path in changed:
            import difflib
            before = git('show', task['base']+':'+path).decode().splitlines(True)
            after = (workspace/path).read_text().splitlines(True)
            patches[path] = ''.join(difflib.unified_diff(before, after, fromfile='a/'+path, tofile='b/'+path))
        return dict(valid_submission=True, changed_paths=changed, regression=regression, suite=suite,
                    task_success=regression['exit_code'] == 0 and suite['exit_code'] == 0, patches=patches)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['controls', 'prepare', 'score'])
    parser.add_argument('--state', type=Path, required=True, help='Coordinator-only output outside participant snapshots')
    parser.add_argument('--workspaces', type=Path, default=Path('/tmp/elephant-historical-trials'))
    parser.add_argument('--go', type=Path, required=True)
    parser.add_argument('--baseline-binary', type=Path)
    parser.add_argument('--model-cache', type=Path)
    parser.add_argument('--allow-model-download', action='store_true')
    parser.add_argument('--trial', help='Score one trial ID, or all when omitted')
    parser.add_argument('--solver-model', default='unspecified')
    args = parser.parse_args()
    args.state.mkdir(parents=True, exist_ok=True)
    definitions = tasks()
    if args.action == 'controls':
        result = {t['id']: controls(t, args.go) for t in definitions}
        (args.state/'controls.json').write_text(json.dumps(result, indent=2)+'\n')
        print(json.dumps({k:v['valid'] for k,v in result.items()}))
        if not all(r['valid'] for r in result.values()):
            raise SystemExit(1)
    elif args.action == 'prepare':
        if (args.state/'manifest.json').exists():
            raise ValueError('refusing to overwrite a frozen manifest')
        control = json.loads((args.state/'controls.json').read_text())
        if not all(control[t['id']]['valid'] for t in definitions):
            raise ValueError('base-red/fixed-green controls required')
        eng = engine()
        models = eng.Models(args.model_cache, args.allow_model_download)
        cfg = types.SimpleNamespace(budget=4000, candidates=10, min_rerank=0.0, rounds=1, elephant_binary=args.baseline_binary)
        trials = {}
        for task in definitions:
            case = corpus(task)
            (args.state/(task['id']+'-corpus.json')).write_text(json.dumps(case, indent=2)+'\n')
            for condition in CONDITIONS:
                result = eng.evaluate(case, condition, cfg, models)
                # Historical tasks have no retrieval gold labels. Never use the smoke
                # evaluator's empty-gold abstention metrics as task-quality scores.
                context = result['context']
                trial_id = task['id']+'-'+condition
                destination = args.workspaces/f'participant-{len(trials)+1:02d}'
                prompt = prepare(task, condition, destination, context, args.go)
                (args.state/(trial_id+'-prompt.txt')).write_text(prompt)
                trials[trial_id] = dict(task_id=task['id'], condition=condition, workspace=str(destination),
                    context=context, context_sha256=sha(context.encode()), context_bytes=len(context.encode()),
                    selected_ids=result['selected_ids'], retrieval_ms=result['elapsed_ms'],
                    trace=result['trace'], baseline_inventory=inventory(destination),
                    prompt_sha256=sha(prompt.encode()), task_success=None, tokens=None, billed_cost=None)
        launch_order = list(trials)
        random.Random(20261031).shuffle(launch_order)
        manifest = dict(schema_version=1, source_revision=git('rev-parse','HEAD').decode().strip(),
                        source_hashes=source_hashes(), helper_sha256=sha(args.baseline_binary.read_bytes()),
                        go_version=subprocess.check_output([str(args.go),'version']).decode().strip(),
                        solver=dict(model=args.solver_model, settings='inherited session defaults; not independently configurable',
                                    tool_budget_enforcement='instruction only', tokens=None, billed_cost=None),
                        launch_order=launch_order, order_seed=20261031,
                        tasks=definitions, models=models.manifest(), trials=trials,
                        configuration=dict(context_bytes=4000,candidates=10,min_rerank=0.0,rounds=1,tool_call_cap=12),
                        reconstructed_lessons=True, isolation='instruction-enforced filesystem boundaries; shared host; not a hardened sandbox')
        (args.state/'manifest.json').write_text(json.dumps(manifest, indent=2)+'\n')
        print(json.dumps({k:{'context_bytes':v['context_bytes'],'selected_ids':v['selected_ids']} for k,v in trials.items()},indent=2))
    else:
        manifest = json.loads((args.state/'manifest.json').read_text())
        if source_hashes() != manifest['source_hashes']:
            raise ValueError('benchmark sources changed after preparation')
        if args.trial and args.trial not in manifest['trials']:
            raise ValueError('unknown trial ID')
        by_id = {t['id']:t for t in manifest['tasks']}
        for key, trial in manifest['trials'].items():
            if args.trial and args.trial != key:
                continue
            result = score(by_id[trial['task_id']], Path(trial['workspace']), trial['baseline_inventory'], args.go)
            (args.state/(key+'-score.json')).write_text(json.dumps(result, indent=2)+'\n')
            print(key,result['task_success'])


if __name__ == '__main__':
    main()
