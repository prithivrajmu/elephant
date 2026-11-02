"""Validate a private authentic-history intake; never upload or execute it."""
import argparse
from datetime import datetime
import hashlib
import json
from pathlib import Path, PurePosixPath
import re


def stamp(value):
    t=datetime.fromisoformat(value.replace('Z','+00:00'))
    if t.tzinfo is None:raise ValueError('timestamps require timezones')
    return t


def validate(data):
    if data.get('classification')!='authentic_private' or data.get('redacted') is not True:
        raise ValueError('Authentic, manually redacted data must be explicitly labelled.')
    if type(data.get('approved_for_external_model')) is not bool:
        raise ValueError('Explicitly state whether selected history may go to the provider.')
    tasks=data.get('tasks',[])
    if not tasks:raise ValueError('Provide at least one completed task.')
    ids=set();groups={};turn_count=0
    for task in tasks:
        if not isinstance(task.get('id'),str) or not task['id'] or task['id'] in ids:raise ValueError('unique task IDs required')
        ids.add(task['id']);split=task.get('split');group=task.get('group')
        if split not in ['dev','reserved'] or not isinstance(group,str) or not group:raise ValueError('split and group required')
        if group in groups and groups[group]!=split:raise ValueError('task group crosses split')
        groups[group]=split
        for k in ['base_revision','fix_revision']:
            if not re.fullmatch('[a-f0-9]{40}',task.get(k,'')):raise ValueError('full Git revision IDs required')
        for k in ['allowed_changes','test_overlay']:
            paths=task.get(k)
            if not isinstance(paths,list) or not paths:raise ValueError('production paths and regression test paths required')
            for path in paths:
                if not isinstance(path,str) or not path:raise ValueError('nonempty file path required')
                p=PurePosixPath(path)
                if p.is_absolute() or '..' in p.parts or '.git' in p.parts or '\\' in path:raise ValueError('unsafe file path')
        if set(task['allowed_changes']) & set(task['test_overlay']):raise ValueError('tests cannot be production allowlist entries')
        if not isinstance(task.get('prompt'),str) or not task['prompt']:raise ValueError('task prompt required')
        cutoff=stamp(task['cutoff']);turns=task.get('history',[]);seen=set()
        if not turns:raise ValueError('earlier history required')
        for turn in turns:
            if not isinstance(turn.get('id'),str) or not turn['id'] or turn['id'] in seen:raise ValueError('unique turn IDs required')
            seen.add(turn['id'])
            if stamp(turn['at'])>=cutoff:raise ValueError('history must precede task cutoff')
            if turn.get('role') not in ['user','assistant','tool'] or not isinstance(turn.get('text'),str) or not turn['text']:raise ValueError('history role/text required')
        turn_count+=len(turns)
    return dict(tasks=len(tasks),groups=len(groups),turns=turn_count,approved_for_external_model=data['approved_for_external_model'],
                validation_scope='schema and cutoff only; authenticity, redaction, repo ancestry and history-dependence require review')


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('file',type=Path);a=p.parse_args()
    raw=a.file.read_bytes();result=validate(json.loads(raw));result['sha256']=hashlib.sha256(raw).hexdigest();print(json.dumps(result,indent=2))
