"""Choose retrieval settings using development fixtures only; no coding-model claim."""
import argparse
import json
from pathlib import Path
from runner import binding
from retrieval import deduplicate, engine


def calibrate(data,models):
    cases=[c for c in data['cases'] if c['split']=='dev']
    if not cases:raise ValueError('development cases required')
    # The reserved split is never passed to retrieval or ranking.
    rankings={}
    for c in cases:
        raw=engine.chunks([dict(d,kind='evidence') for d in c['evidence'] if engine.eligible(d,c)])
        for short in [False,True]:
            trace={};q=c['retrieval_query'] if short else c['query']
            ranked=engine.hybrid(q,raw,models,10,float('-inf'),trace)
            rankings[(c['id'],short)]=(ranked,trace['reranker_scores'])
    candidates=[]
    for short in [False,True]:
        for threshold in [0.0,-2.0,-5.0]:
            hits=used=0
            for c in cases:
                ranked,scores=rankings[(c['id'],short)]
                text,picked,_=engine.pack(deduplicate([d for d in ranked if scores[d['chunk_id']]>=threshold]),4000)
                hits+=set(c['gold_evidence']) <= {d['id'] for d in picked};used+=len(text.encode())
            candidates.append(dict(short_query=short,threshold=threshold,deduplicate=True,
                                   complete_retrieval=hits,cases=len(cases),context_bytes=used))
    best=max(candidates,key=lambda c:(c['complete_retrieval'],-c['context_bytes'],c['threshold'],c['short_query']))
    return dict(binding=binding(),split='dev',case_ids=[c['id'] for c in cases],
                objective='maximize complete raw retrieval, then minimize context bytes; not coding success',
                selected={k:best[k] for k in ['short_query','threshold','deduplicate']},candidates=candidates,models=models.manifest())


if __name__=='__main__':
    p=argparse.ArgumentParser();p.add_argument('--model-cache',type=Path,required=True);p.add_argument('--state',type=Path,required=True);a=p.parse_args()
    models=engine.Models(a.model_cache);data=json.loads(Path(__file__).with_name('fixtures.json').read_text())
    result=calibrate(data,models);a.state.mkdir(parents=True,exist_ok=True)
    if (a.state/'calibration.json').exists():raise ValueError('calibration already frozen')
    (a.state/'calibration.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result['selected']))
