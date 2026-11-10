"""Synthetic history-dependent contract repairs, never authentic workflow data."""
import hashlib
import json
import random


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def make():
    cases = []
    for split, count, seed in [('dev', 12, 19743), ('reserved', 24, 59183)]:
        rng = random.Random(seed)
        for n in range(count):
            project = f'{split}-route-{n:02}'
            # Independent selections; the answer is absent from task/source.
            contract = dict(codec=rng.choice(['utf8', 'ascii', 'base64', 'hex']),
                            retries=rng.randrange(1, 10), mode=rng.choice(['strict', 'replace', 'ignore']))
            base = dict(tenant='fixture', owner='tester', project=project, scope='project',
                        evidence_type='observation', occurred_at='2026-09-01T00:00:00Z', source=f'fixture://{project}/release')
            text = f'Recorded release decision for {project}: compatibility contract = '+json.dumps(contract, sort_keys=True)
            raw = dict(base, id='decision', text=text, sha256=hashlib.sha256(text.encode()).hexdigest())
            # Some contracts intentionally have no extracted lesson.
            lesson = dict(raw, id='lesson', incident=f'Release contract for {project}',
                          evidence_type='interpretation', evidence_ids=['decision'], features={})
            distractors = [dict(base, id=f'distractor-{i}', source=f'fixture://{project}/unrelated-{i}',
                               text=f'Unrelated release note {i}: cache maintenance completed for {project}.') for i in range(8)]
            for d in distractors: d['sha256'] = hashlib.sha256(d['text'].encode()).hexdigest()
            cases.append(dict(id=project, group=project, split=split, category='synthetic_contract_repair',
                              identity={'tenant':'fixture','user':'tester'}, project=project, features={},
                              at='2026-10-01T00:00:00Z',
                              query=f'Restore the recorded release compatibility contract for {project}. Implement contract() in policy.py; preserve its exact values. Return a literal dictionary.',
                              retrieval_query=f'{project} recorded release compatibility contract',
                              files={'policy.py':'def contract():\n    return None\n'},
                              evidence=[raw]+distractors, lessons=[] if n % 4 == 0 else [lesson],
                              gold_evidence=['decision'], gold_lessons=[] if n % 4 == 0 else ['lesson'],
                              expected=contract))
    return dict(schema_version=1, classification='synthetic; shared task template; not independent generalization evidence', cases=cases)


if __name__ == '__main__':
    from pathlib import Path
    Path(__file__).with_name('fixtures.json').write_text(json.dumps(make(), indent=2)+'\n')
