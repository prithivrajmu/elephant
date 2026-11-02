"""Prepare provider configuration and locked run. Never makes a paid API call."""
import argparse
import json
import math
import os
from pathlib import Path
import subprocess
import sys

HERE=Path(__file__).resolve().parent


def configure(state,model,pricing,helper,cache,settings,max_estimated_usd=5.0):
    if not math.isfinite(max_estimated_usd) or max_estimated_usd<=0:raise ValueError('positive estimate limit required')
    if not os.environ.get('OPENAI_API_KEY'):raise ValueError('Set OPENAI_API_KEY in your local environment; do not send it in chat.')
    if not model or model=='EXACT_PROVIDER_SNAPSHOT':raise ValueError('Choose an exact API model snapshot ID.')
    if not helper.is_file() or not cache.is_dir():raise ValueError('Build recall helper and prepare local model cache first.')
    if state.exists():raise ValueError('Choose a new state directory.')
    import openai_adapter as adapter
    rates=json.loads(pricing.read_text())
    # Validate settings and rate card locally without network calls.
    payload=dict(model_id=model,model_settings=settings,task='',files={},context='',instructions='')
    adapter.request_body(payload)
    adapter.normalize(dict(id='local-preflight',model=model,status='completed',usage={'input_tokens':0,'output_tokens':0},output=[]),payload,rates)
    state.mkdir(parents=True)
    lock=dict(runtime_files=[str(HERE/'openai_adapter.py')],model_settings=settings,python_version=sys.version)
    (state/'adapter-lock.json').write_text(json.dumps(lock,indent=2)+'\n')
    argv=[sys.executable,str(HERE/'openai_adapter.py'),'--pricing',str(pricing.resolve())]
    commands=[
        [sys.executable,str(HERE/'runner.py'),'controls','--state',str(state)],
        [sys.executable,str(HERE/'calibrate.py'),'--state',str(state),'--model-cache',str(cache)],
        [sys.executable,str(HERE/'runner.py'),'prepare','--state',str(state),'--model-id',model,
         '--adapter-command',json.dumps(argv),'--adapter-lock',str(state/'adapter-lock.json'),
         '--helper',str(helper),'--model-cache',str(cache),'--repeats','3','--max-estimated-usd',str(max_estimated_usd)]]
    for command in commands:subprocess.run(command,check=True)
    print('Prepared 360-call reserved run. No paid API calls made. Run runner.py run with this state directory when ready.')


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__)
    for key in ['state','pricing','helper','model-cache']:p.add_argument('--'+key,type=Path,required=True)
    p.add_argument('--max-estimated-usd',type=float,required=True);p.add_argument('--model-id',required=True);p.add_argument('--model-settings',default='{"max_output_tokens":2048}')
    a=p.parse_args()
    try:configure(a.state.resolve(),a.model_id,a.pricing.resolve(),a.helper.resolve(),a.model_cache.resolve(),json.loads(a.model_settings),a.max_estimated_usd)
    except (ValueError,OSError):print('Setup incomplete: check credential presence, explicit model/pricing/settings, helper/cache and a new state directory.',file=sys.stderr);sys.exit(1)
