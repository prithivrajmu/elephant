"""Experimental retrieval choices; the historical pilot engine stays frozen."""
import copy
import importlib.util
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('history_engine', ROOT/'experiments/memory_tax/run.py')
engine = importlib.util.module_from_spec(spec)
spec.loader.exec_module(engine)


def deduplicate(ranked):
    """Skip overlapping representations, preserving disjoint raw chunks."""
    selected, raw_spans, lesson_refs = [], {}, set()
    for d in ranked:
        if d['kind'] == 'lesson':
            refs = set(d.get('evidence_ids', []))
            if refs & (raw_spans.keys() | lesson_refs):
                continue
            lesson_refs.update(refs)
        else:
            if d['id'] in lesson_refs:
                continue
            spans = raw_spans.setdefault(d['id'], [])
            a, b = d['span']
            if any(a < end and start < b for start, end in spans):
                continue
            spans.append((a, b))
        selected.append(d)
    return selected


def context(case, condition, cfg, models):
    c = copy.deepcopy(case)
    if cfg['short_query']: c['query'] = c['retrieval_query']
    if condition in ('none', 'elephant', 'raw_bm25'):
        import types
        row = engine.evaluate(c, condition, types.SimpleNamespace(budget=4000,candidates=10,rounds=1,
                              min_rerank=cfg['threshold'],elephant_binary=Path(cfg['helper'])), models)
        return row['context'], row['selected_ids']
    raw = engine.chunks([dict(d,kind='evidence') for d in c['evidence'] if engine.eligible(d,c)])
    lessons = engine.chunks([dict(d,kind='lesson') for d in c['lessons'] if engine.eligible(d,c,True)])
    trace = {}
    ranked = engine.hybrid(c['query'],raw+(lessons if condition=='combined' else []),models,10,cfg['threshold'],trace)
    if cfg['deduplicate']: ranked = deduplicate(ranked)
    text, picked, _ = engine.pack(ranked,4000)
    return text, sorted({d['id'] for d in picked})
