"""Prepare/run real host acceptance without bypassing hook trust or permissions.

Prepare, open the printed project in your host, approve Elephant hooks normally,
then rerun with --run. The fixture is isolated synthetic engineering work.
"""
import argparse
import json
import pathlib
import shutil
import subprocess
import sys

p = argparse.ArgumentParser()
p.add_argument('binary')
p.add_argument('--agent', choices=['codex', 'claude'], required=True)
p.add_argument('--workspace', required=True)
p.add_argument('--host', default='')
p.add_argument('--run', action='store_true')
a = p.parse_args()
binary = str(pathlib.Path(a.binary).resolve())
workspace = pathlib.Path(a.workspace).resolve()
project = workspace / a.agent
project.mkdir(parents=True, exist_ok=True)
store = project / 'events.jsonl'

def elephant(command, *args):
    return json.loads(subprocess.check_output([binary, command, '--root', str(project), *args], text=True))

if not a.run:
    assert not store.exists(), 'Choose a fresh workspace to avoid old evidence.'
    (project / 'workers.py').write_text('def worker_count(requested, capacity):\n    return requested\n')
    (project / 'test_workers.py').write_text('from workers import worker_count\nassert worker_count(8, 3) == 3\nassert worker_count(2, 3) == 2\nprint("worker capacity checks passed")\n')
    elephant('init', '--agent', a.agent, '--project', 'native-'+a.agent,
             '--store', str(store), '--tenant', 'acceptance', '--user', 'tester')
    print(f'Open {project} in {a.agent}; approve its hooks normally. Then rerun this command with --run.')
    sys.exit(0)

host = a.host or shutil.which(a.agent)
assert host, f'{a.agent} is not installed.'

def task(prompt):
    if a.agent == 'codex':
        command = [host, 'exec', '--ephemeral', '--skip-git-repo-check', '-C', str(project),
                   '--sandbox', 'workspace-write', '--json', prompt]
    else:
        command = [host, '-p', '--output-format', 'json', prompt]
    # Uses existing login and normal host permission/trust policy. No bypass flags.
    try:
        result = subprocess.run(command, cwd=project, text=True, capture_output=True, timeout=180)
    except subprocess.TimeoutExpired as exc:
        output = exc.stdout or b''
        if isinstance(output, bytes): output = output.decode(errors='replace')
        (workspace / (a.agent+'-last-session.log')).write_text(output)
        raise SystemExit('Native host timed out. Inspect the local session log and host setup; no acceptance is claimed.')
    (workspace / (a.agent+'-last-session.log')).write_text(result.stdout+'\n'+result.stderr)
    assert result.returncode == 0, f'Host failed; inspect {a.agent}-last-session.log locally.'
    return result.stdout

first = task('Run python3 test_workers.py. Fix workers.py so worker_count respects capacity and preserves requests below capacity. Run the checks again and briefly report the result.')
subprocess.run(['python3','test_workers.py'],cwd=project,check=True)
d = elephant('status')
saved = [m for m in d['memories'] if m['project']=='native-'+a.agent and not m['retired']]
assert saved, 'No lesson was saved automatically. Check actual hook approval/events and capture misses.'
assert any(x['status']=='memory_saved' for x in d['experiences']), 'Missing acknowledged task receipt.'
assert 'Elephant: recalled ' in first, 'Missing one-line user status.'

second = task('Inspect worker_count for a new use case: requested workers must never exceed the available worker capacity. Run its checks and explain whether the current implementation is suitable.')
after = elephant('status')
assert any(m['id']==saved[0]['id'] for m in after['memories']), 'Memory did not survive host restart.'
assert any(saved[0]['id'] in u['memory_ids'] for u in after['usage']), 'Related task did not recall the saved lesson.'
unrelated = elephant('recall','--task','choose a color palette for a birthday invitation')
assert not unrelated['hits'], 'Unrelated recall did not abstain.'
print(json.dumps({'agent':a.agent,'host_version':subprocess.check_output([host,'--version'],text=True).strip(),
                  'real_tasks':2,'automatic_save':True,'restart_recall':True,'unrelated_abstention':True,
                  'review_quality':'inspect actual saved sources and conditions; not proved by this script'}))
