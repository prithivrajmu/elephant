#!/usr/bin/env python3
"""Coordinator-owned patch-only benchmark. Never executes generated Python."""
import argparse
import ast
import hashlib
import json
import math
import os
from pathlib import Path
import random
import subprocess
import tempfile

from fixtures import digest
from retrieval import context, engine

HERE = Path(__file__).resolve().parent
CONDITIONS = ['none','elephant','raw_bm25','raw_hybrid','combined']


def sources():
    paths = ['fixtures.py','fixtures.json','runner.py','retrieval.py','calibrate.py']
    result = {p:hashlib.sha256((HERE/p).read_bytes()).hexdigest() for p in paths}
    result['memory_tax/run.py'] = hashlib.sha256((HERE.parent/'memory_tax/run.py').read_bytes()).hexdigest()
    return result


def binding():
    return digest(sources())


def evaluate(files, expected):
    # No eval/exec/import or candidate-controlled test code. A restricted AST is
    # interpreted as a literal contract, not run as a Python program.
    if not isinstance(files,dict) or set(files) != {'policy.py'}:
        raise ValueError('only policy.py may be returned')
    text = files['policy.py']
    if not isinstance(text,str) or len(text.encode()) > 4096:
        raise ValueError('patch exceeds byte budget')
    try:
        tree = ast.parse(text)
        if len(tree.body)!=1 or not isinstance(tree.body[0],ast.FunctionDef):return False
        fn = tree.body[0]
        if fn.name!='contract' or fn.decorator_list or fn.returns or fn.type_comment:return False
        args = fn.args
        if args.posonlyargs or args.args or args.kwonlyargs or args.vararg or args.kwarg:return False
        if len(fn.body)!=1 or not isinstance(fn.body[0],ast.Return):return False
        # Require precisely the string/integer fields; bool is not an integer here.
        value = ast.literal_eval(fn.body[0].value)
        if not isinstance(value,dict) or set(value)!=set(expected):return False
        return all(type(value[k]) is type(expected[k]) and value[k]==expected[k] for k in expected)
    except (SyntaxError,ValueError,TypeError,RecursionError):
        return False


def controls(data):
    rows = []
    for c in data['cases']:
        wrong = evaluate(c['files'],c['expected'])
        correct = evaluate({'policy.py':'def contract():\n    return '+repr(c['expected'])+'\n'},c['expected'])
        rows.append(dict(id=c['id'],base_success=wrong,reference_success=correct))
    return dict(binding=binding(),valid=all(not r['base_success'] and r['reference_success'] for r in rows),rows=rows)


def adapter_call(argv, payload, model):
    # Transport is trusted host code; model output is data only. No model tool API
    # or file paths to the coordinator/evaluator are exposed in the request.
    with tempfile.TemporaryFile() as out, tempfile.TemporaryFile() as err:
        kwargs = {}
        if os.name == 'posix':
            import resource
            def limit_output(): resource.setrlimit(resource.RLIMIT_FSIZE,(65536,65536))
            kwargs['preexec_fn'] = limit_output
        env={k:os.environ[k] for k in ['PATH','LANG','LC_ALL','SYSTEMROOT','OPENAI_API_KEY','ANTHROPIC_API_KEY'] if k in os.environ}
        env.update(ORT_DISABLE_TELEMETRY='1',HF_HUB_DISABLE_TELEMETRY='1')
        p = subprocess.run(argv,input=json.dumps(payload).encode(),stdout=out,stderr=err,timeout=120,env=env,**kwargs)
        if p.returncode: raise RuntimeError('adapter process failed; stderr retained by transport, not exposed')
        if out.tell()>65536:raise ValueError('adapter response too large')
        out.seek(0); response=json.load(out)
    if not isinstance(response,dict) or response.get('model_id')!=model:
        raise ValueError('adapter changed pinned model identity')
    if 'model_settings' in payload and response.get('model_settings')!=payload['model_settings']:
        raise ValueError('adapter changed pinned decoding settings')
    usage = response.get('usage',{})
    if not isinstance(usage,dict) or any(type(usage.get(k)) is not int or usage[k]<0 for k in ['input_tokens','output_tokens']):
        raise ValueError('adapter requires measured token counts')
    cost = response.get('billed_cost_usd')
    if type(cost) not in (int,float) or not math.isfinite(cost) or cost<0:
        raise ValueError('adapter requires explicit nonnegative billed_cost_usd')
    if not isinstance(response.get('files'),dict):raise ValueError('adapter requires files')
    return response


def adapter_identity(argv, lock):
    if not isinstance(argv,list) or not argv or not all(isinstance(x,str) for x in argv):
        raise ValueError('adapter command must be a JSON argv array')
    import shutil
    executable=shutil.which(argv[0])
    if not executable:raise ValueError('adapter executable unavailable')
    if not isinstance(lock,dict) or not isinstance(lock.get('runtime_files'),list) or not lock['runtime_files'] or not isinstance(lock.get('model_settings'),dict):
        raise ValueError('adapter lock requires runtime_files and explicit model_settings')
    paths=[Path(executable)]+[Path(x) for x in argv[1:] if Path(x).is_file()]+[Path(x) for x in lock['runtime_files']]
    if any(not p.is_file() for p in paths):raise ValueError('adapter runtime dependency missing')
    return dict(argv=argv,files={str(p.resolve()):hashlib.sha256(p.read_bytes()).hexdigest() for p in paths})


def prepare(data, controls_result, calibration, helper, model, argv, repeats, lock):
    if controls_result != controls(data) or controls_result.get('valid') is not True:
        raise ValueError('controls do not match current sources/data')
    dev_ids=[c['id'] for c in data['cases'] if c['split']=='dev']
    if (calibration.get('binding')!=binding() or calibration.get('split')!='dev'
        or calibration.get('case_ids')!=dev_ids
        or not any(all(candidate.get(k)==v for k,v in calibration.get('selected',{}).items()) for candidate in calibration.get('candidates',[]))
        or set(calibration.get('selected',{})) != {'short_query','threshold','deduplicate'}):
        raise ValueError('development calibration does not match sources/data')
    if repeats<2 or not model.strip():raise ValueError('require repeated runs and explicit pinned model ID')
    config=dict(calibration['selected'],helper=str(helper.resolve()))
    cells=[dict(case=c['id'],condition=condition,repeat=i) for c in data['cases'] if c['split']=='reserved'
           for condition in CONDITIONS for i in range(repeats)]
    random.Random(81293).shuffle(cells)
    return dict(schema_version=1,binding=binding(),model_id=model,adapter=adapter_identity(argv,lock),adapter_lock=lock,
                helper_sha256=hashlib.sha256(helper.read_bytes()).hexdigest(),configuration=config,cells=cells,
                dataset_sha256=digest(data),controls=controls_result,calibration=calibration,
                controls_sha256=digest(controls_result),calibration_sha256=digest(calibration),
                budget=dict(context_bytes=4000,patch_bytes=4096,model_calls_per_cell=1,model_tools=0),
                classification='synthetic protocol experiment; no arbitrary code execution')


def run(data, manifest, output):
    if manifest['binding']!=binding() or manifest['dataset_sha256']!=digest(data):
        raise ValueError('manifest sources or data changed')
    if adapter_identity(manifest['adapter']['argv'],manifest['adapter_lock'])!=manifest['adapter']:
        raise ValueError('adapter changed after freeze')
    if (manifest['controls']!=controls(data) or digest(manifest['controls'])!=manifest['controls_sha256']
        or digest(manifest['calibration'])!=manifest['calibration_sha256']):
        raise ValueError('bound control/calibration artifacts changed')
    cfg=manifest['configuration']
    if any(cfg.get(k)!=v for k,v in manifest['calibration']['selected'].items()):
        raise ValueError('retrieval configuration changed after calibration')
    if hashlib.sha256(Path(cfg['helper']).read_bytes()).hexdigest()!=manifest['helper_sha256']:
        raise ValueError('retrieval helper changed')
    # New output only; interrupted runs cannot silently overwrite or cherry-pick.
    output.mkdir(parents=True,exist_ok=False)
    cases={c['id']:c for c in data['cases']};models=engine.Models(Path(manifest['model_cache']))
    if models.manifest()!=manifest['models']:raise ValueError('retrieval model artifacts changed')
    # Compute context once per case/condition. Repeats receive byte-identical input.
    cached={}
    (output/'status.json').write_text(json.dumps(dict(status='running',expected=len(manifest['cells']),completed=0)))
    for i,cell in enumerate(manifest['cells']):
        c=cases[cell['case']]; key=(c['id'],cell['condition'])
        if key not in cached:cached[key]=context(c,cell['condition'],cfg,models)
        text,ids=cached[key]
        payload=dict(operation='repair',model_id=manifest['model_id'],task=c['query'],files=c['files'],
                     context=text,model_settings=manifest['adapter_lock']['model_settings'],instructions='Return files containing only policy.py. No tools. Use a literal contract dictionary.')
        if len(text.encode())>4000:raise ValueError('context byte budget exceeded')
        response=adapter_call(manifest['adapter']['argv'],payload,manifest['model_id'])
        try: success=evaluate(response['files'],c['expected']);invalid=None
        except ValueError as e:success=False;invalid=str(e)
        row=dict(cell,success=success,invalid_submission=invalid,context_sha256=hashlib.sha256(text.encode()).hexdigest(),
                 context_bytes=len(text.encode()),selected_ids=ids,response=response)
        (output/f'{i:04}.json').write_text(json.dumps(row,indent=2)+'\n')
        (output/'status.json').write_text(json.dumps(dict(status='complete' if i+1==len(manifest['cells']) else 'running',expected=len(manifest['cells']),completed=i+1)))


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('action',choices=['controls','prepare','run'])
    p.add_argument('--adapter-lock',type=Path);p.add_argument('--state',type=Path,required=True);p.add_argument('--model-id');p.add_argument('--adapter-command')
    p.add_argument('--helper',type=Path);p.add_argument('--model-cache',type=Path);p.add_argument('--repeats',type=int,default=3)
    a=p.parse_args();data=json.loads((HERE/'fixtures.json').read_text());a.state.mkdir(parents=True,exist_ok=True)
    if a.action=='controls':
        result=controls(data);(a.state/'controls.json').write_text(json.dumps(result,indent=2)+'\n');print(result['valid'])
    elif a.action=='prepare':
        if (a.state/'manifest.json').exists():raise ValueError('manifest already frozen')
        if not all([a.model_id,a.adapter_command,a.helper,a.model_cache,a.adapter_lock]):p.error('explicit model, adapter, helper and model cache required')
        result=prepare(data,json.loads((a.state/'controls.json').read_text()),json.loads((a.state/'calibration.json').read_text()),a.helper,a.model_id,json.loads(a.adapter_command),a.repeats,json.loads(a.adapter_lock.read_text()))
        models=engine.Models(a.model_cache)
        calibration=json.loads((a.state/'calibration.json').read_text())
        if calibration['models']!=models.manifest():raise ValueError('calibration retrieval model artifacts changed')
        result.update(model_cache=str(a.model_cache.resolve()),models=models.manifest())
        (a.state/'manifest.json').write_text(json.dumps(result,indent=2)+'\n')
        (a.state/'manifest.sha256').write_text(digest(result)+'\n')
    else:
        manifest=json.loads((a.state/'manifest.json').read_text())
        if digest(manifest)!=(a.state/'manifest.sha256').read_text().strip():raise ValueError('manifest changed after freeze')
        run(data,manifest,a.state/'trials')


if __name__=='__main__':main()
