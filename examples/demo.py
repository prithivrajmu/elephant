"""Build an isolated, explicitly synthetic demo. No model calls or real project data."""
import argparse
import datetime as dt
import json
import pathlib
import subprocess

parser = argparse.ArgumentParser()
parser.add_argument('--binary', default='./elephant')
parser.add_argument('--dir', default='.demo', help='isolated synthetic demo directory')
args = parser.parse_args()
binary = str(pathlib.Path(args.binary).resolve())
demo = pathlib.Path(args.dir).resolve()
root = demo / 'project'
store = demo / 'events.jsonl'
if store.exists():
    raise SystemExit('Demo already exists; delete that isolated directory explicitly to reset it.')
root.mkdir(parents=True, exist_ok=True)
features = {'language': ['typescript'], 'framework': ['nextjs'], 'database': ['snowflake'],
            'cloud': ['aws'], 'app': ['analytics-dashboard'], 'style': ['synthetic-demo']}
(root / '.elephant.json').write_text(json.dumps({'features': features}, indent=2))
common = ['--root', str(root), '--project', 'demo-dashboard', '--store', str(store), '--team', 'demo-platform']

def run(command, extra=(), body=None):
    result = subprocess.run([binary, command, *common, *extra], input=json.dumps(body) if body is not None else None,
                            text=True, capture_output=True, check=True)
    return json.loads(result.stdout) if result.stdout.strip() else None

lessons = [
 ('worst', 'A burst of requests exhausted available warehouse connections.', 'Bound query concurrency to available pool capacity. Measure queue wait and cancellation before increasing the limit.', 'warehouse-concurrency'),
 ('great', 'Repeated filter selections caused duplicate queries.', 'Coalesce in-flight requests with identical normalized filters and user visibility scope to avoid duplicate work.', 'query-coalescing'),
 ('bad', 'Two equivalent filters produced different cache keys.', 'Canonicalize filter ordering and missing values before computing cache keys. Include access scope and data freshness.', 'filter-cache-keys'),
 ('good', 'A malformed query reached the warehouse.', 'Validate query inputs at the server boundary with a typed schema, and allowlist identifiers separately from bound values.', 'query-validation'),
 ('great', 'Slow loading became measurable after instrumentation.', 'Track queue wait, warehouse duration and response time separately to find where a request spends its time.', 'observability'),
 ('bad', 'A shared cache leaked a result across user scopes.', 'Include authorization scope in result cache keys and validate access before consulting shared caches.', 'cache-access'),
 ('good', 'A reviewer could not verify an incremental change.', 'Attach a reproducible validation command and a concrete before/after example to a performance PR.', 'review-evidence'),
 ('worst', 'A retried workflow applied one update twice.', 'Give retried mutations an idempotency key and verify duplicate delivery does not repeat the effect.', 'retry-idempotency'),
 ('good', 'Source schema changed without a clear error.', 'Test source contract assumptions and surface schema mismatches before downstream dashboard calculations run.', 'source-contract'),
 ('great', 'Independent requests loaded faster under a bounded worker queue.', 'Parallelize independent queries with a measured concurrency cap. Preserve cancellation and aggregate partial errors explicitly.', 'query-parallelization'),
 ('bad', 'A middleware bypass removed an access check.', 'Test user authorization on the serving path when introducing a mock bypass or alternate query route.', 'route-authorization'),
 ('good', 'A refresh strategy left stale results after new data arrived.', 'Tie cache invalidation to the data freshness boundary and test cross-filter behavior after refresh.', 'cache-freshness'),
]
ids = []
for i, (outcome, incident, lesson, subject) in enumerate(lessons):
    record = run('record', body={'scope': 'team' if i >= 10 else 'personal', 'outcome': outcome,
        'incident': 'SYNTHETIC DEMO: ' + incident, 'lesson': lesson, 'source': f'SYNTHETIC DEMO / example-{i+1:02}',
        'subject': subject, 'features': features, 'requires': {'database': ['snowflake']}})
    ids.append(record['id'])
    if i == 10:
        run('approve', ['--id', record['id']])

# Backdate synthetic creations only, to demonstrate growth. Never edit a real journal this way.
events = [json.loads(line) for line in store.read_text().splitlines()]
now = dt.datetime.now(dt.timezone.utc)
for event in events:
    if event['kind'] == 'put':
        i = ids.index(event['memory']['id'])
        created = (now - dt.timedelta(days=13-i)).isoformat().replace('+00:00', 'Z')
        event['memory']['created'] = created
        event['memory']['updated'] = created
        event['at'] = created
store.write_text(''.join(json.dumps(e) + '\n' for e in events))
for i in range(7):
    run('recall', ['--task', ['query concurrency connections', 'cache filter authorization', 'performance review tests'][i % 3],
                   '--budget', '1600', '--limit', '3'])
for i in range(6):
    run('feedback', ['--id', ids[i], '--feedback-id', f'synthetic-task-{i}', '--helpful=' + ('false' if i == 5 else 'true')])
print('SYNTHETIC DEMO created. Invented data; not an effectiveness benchmark.')
print(f'{binary} ui --root {root} --project demo-dashboard --store {store} --team demo-platform')
