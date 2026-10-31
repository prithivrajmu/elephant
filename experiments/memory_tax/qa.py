"""Explicit subprocess QA/scoring adapters; no gold labels are sent to QA."""
import json
import math
import subprocess
import time


def call(argv, request):
    if not isinstance(argv, list) or not argv or not all(isinstance(s, str) for s in argv):
        raise ValueError('adapter command must be a nonempty JSON argv array')
    start = time.perf_counter()
    result = subprocess.run(argv, input=json.dumps(request), text=True,
                            capture_output=True, check=True, timeout=120)
    response = json.loads(result.stdout)
    if not isinstance(response, dict) or not isinstance(response.get('model_id'), str) or not response.get('model_id'):
        raise ValueError('adapter must return model_id and JSON object')
    return response, (time.perf_counter()-start)*1000


def answer(row, case, args, models, engine):
    """At most two retrieval rounds, under one cumulative context byte budget."""
    trace = []
    context = row['context']
    prompt = dict(operation='answer', query=case['query'], context=context,
                  current_time=case['at'], round=1,
                  allow_followup=args.rounds == 2 and row['condition'] in ('raw_bm25', 'raw_hybrid', 'combined', 'reserved', 'semantic_lessons'))
    response, elapsed = call(args.qa_command, prompt)
    if not isinstance(response.get('answer'), str):
        raise ValueError('QA adapter must return answer string')
    model_id = response['model_id']
    trace.append(dict(response=response, elapsed_ms=elapsed, context_bytes=len(context.encode())))
    query = response.get('followup_query')
    if query:
        if not prompt['allow_followup'] or not isinstance(query, str) or len(query) > 4096:
            raise ValueError('unexpected or invalid follow-up query')
        condition = row['condition']
        pool = []
        if condition != 'semantic_lessons':
            pool += engine.chunks([dict(d, kind='evidence') for d in case['evidence'] if engine.eligible(d, case)])
        if condition in ('combined', 'reserved', 'semantic_lessons'):
            pool += engine.chunks([dict(d, kind='lesson') for d in case['lessons'] if engine.eligible(d, case, True)])
        pool = [d for d in pool if d['chunk_id'] not in row['selected_chunk_ids']]
        retrieval_trace = {}
        begin = time.perf_counter()
        ranked = (engine.bm25(query, pool, args.candidates) if condition == 'raw_bm25'
                  else engine.hybrid(query, pool, models, args.candidates, args.min_rerank, retrieval_trace))
        new_context, selected, omitted = engine.pack(ranked, args.budget - 2 * row['context_bytes'])
        context += new_context
        used = len(context.encode())
        if used > args.budget:
            raise ValueError('cumulative follow-up context exceeds budget')
        row['context_bytes'] = used
        row['context'] = context
        row['selected_ids'] = sorted(set(row['selected_ids']) | {d['id'] for d in selected})
        row['selected_chunk_ids'] += [d['chunk_id'] for d in selected]
        row['budget_omissions'] += omitted
        row['trace']['followup'] = dict(query=query, retrieval=retrieval_trace,
                                      elapsed_ms=(time.perf_counter()-begin)*1000)
        # Recompute evidence retrieval diagnostics after the second round.
        all_pool = engine.chunks([dict(d, kind='evidence') for d in case['evidence'] if engine.eligible(d, case)])
        all_pool += engine.chunks([dict(d, kind='lesson') for d in case['lessons'] if engine.eligible(d, case, True)])
        picked = [d for d in all_pool if d['chunk_id'] in row['selected_chunk_ids']]
        metrics = engine.measure(case, condition, context, picked, row['budget_omissions'], row['trace'], begin, args.budget)
        for name in ('retrieval_complete', 'retrieval_hit', 'evidence_recall', 'linked_evidence_coverage', 'abstained'):
            row[name] = metrics[name]
        response, elapsed = call(args.qa_command, dict(prompt, context=context, round=2, allow_followup=False))
        if response['model_id'] != model_id or not isinstance(response.get('answer'), str) or response.get('followup_query'):
            raise ValueError('second QA round changed model, lacks answer or requested third round')
        trace.append(dict(response=response, elapsed_ms=elapsed, context_bytes=len(context.encode())))
    row['model_context_exposure_bytes'] = sum(c['context_bytes'] for c in trace)
    if row['model_context_exposure_bytes'] > args.budget:
        raise ValueError('total model context exposure exceeds budget')
    row['qa'] = dict(model_id=model_id, answer=response['answer'], calls=trace)
    # Unknown usage stays unknown, never reported as free.
    for key in ('provider_tokens', 'billed_cost'):
        values = [c['response'].get(key) for c in trace]
        if all(isinstance(v, (int, float)) and math.isfinite(v) and v >= 0 for v in values):
            row[key] = sum(values)
    if args.score_command:
        scored, elapsed = call(args.score_command, dict(operation='score', case=case, answer=response['answer']))
        if type(scored.get('success')) is not bool:
            raise ValueError('scorer success must be boolean')
        row['task_success'] = scored['success']
        row['score'] = dict(response=scored, elapsed_ms=elapsed)
    row['total_elapsed_ms'] = row['elapsed_ms'] + sum(c['elapsed_ms'] for c in trace) + row['trace'].get('followup', {}).get('elapsed_ms', 0) + row.get('score', {}).get('elapsed_ms', 0)
    return row
