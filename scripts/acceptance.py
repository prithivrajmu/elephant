#!/usr/bin/env python3
"""External stdio MCP + concurrent process acceptance. Synthetic isolated store only."""
import concurrent.futures, json, pathlib, subprocess, sys, tempfile, urllib.request, time
BINARY=str(pathlib.Path(sys.argv[1]).resolve())
class Client:
    def __init__(self, args):
        self.proc=subprocess.Popen([BINARY,'mcp',*args],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        self.seq=0
        r=self.call('initialize',{'protocolVersion':'2025-06-18','clientInfo':{'name':'elephant-acceptance','version':'1'},'capabilities':{}})
        assert r['serverInfo']['name']=='elephant'
        self.proc.stdin.write(json.dumps({'jsonrpc':'2.0','method':'notifications/initialized'})+'\n');self.proc.stdin.flush()
    def call(self,method,params=None):
        self.seq+=1
        self.proc.stdin.write(json.dumps({'jsonrpc':'2.0','id':self.seq,'method':method,'params':params or {}})+'\n');self.proc.stdin.flush()
        line=self.proc.stdout.readline()
        assert line, self.proc.stderr.read()
        r=json.loads(line);assert r['id']==self.seq and 'error' not in r,r
        return r['result']
    def tool(self,name,args):
        r=self.call('tools/call',{'name':name,'arguments':args});assert not r['isError'],r
        return r['content'][0]['text']
    def close(self):
        self.proc.stdin.close();assert self.proc.wait(timeout=5)==0;assert not self.proc.stderr.read()
def schema_check(s):
    assert s.get('type') in ('object','array','string','integer','boolean')
    if 'required' in s: assert isinstance(s['required'],list) and all(x in s.get('properties',{}) for x in s['required'])
    for child in s.get('properties',{}).values():schema_check(child)
    if 'items' in s:schema_check(s['items'])
    if isinstance(s.get('additionalProperties'),dict):schema_check(s['additionalProperties'])
with tempfile.TemporaryDirectory(prefix='elephant acceptance ') as tmp:
    root=pathlib.Path(tmp);store=root/'events.jsonl'
    args=['--unattached','--conversation','session-1','--store',str(store),'--tenant','test','--user','alice']
    def cli(command,extra=()):
        p=subprocess.run([BINARY,command,*args,*extra],text=True,capture_output=True,check=True)
        return json.loads(p.stdout) if p.stdout.strip() else None
    assert cli('selftest')['ok'] and cli('doctor')['ok']
    setup=cli('setup',['--output',str(root/'setup with spaces')])['config']
    cfg=setup['mcp_config']['mcpServers']['elephant'];assert pathlib.Path(cfg['command']).is_absolute()
    # Standard JSON quoted paths/arrays are also valid TOML basic strings.
    import tomllib
    assert tomllib.loads(setup['codex_toml'])['mcp_servers']['elephant']['args']==cfg['args']
    c=Client(args);tools=c.call('tools/list')['tools'];assert len(tools)==6
    for t in tools:schema_check(t['inputSchema'])
    assert len(c.call('prompts/list')['prompts'])==2
    assert 'validation' in c.call('prompts/get',{'name':'initmemory','arguments':{'task':'validation'}})['messages'][0]['content']['text']
    assert 'No relevant' in c.tool('init_memory',{'task':'validation evidence'})
    m=json.loads(c.tool('record_memory',{'outcome':'good','incident':'SYNTHETIC acceptance review','lesson':'Attach validation evidence before review','source':'SYNTHETIC test','requires':{'database':['postgres']}}))
    assert 'No relevant' in c.tool('recall_memory',{'task':'validation evidence'})
    assert m['id'] in c.tool('recall_memory',{'task':'validation evidence','context_features':{'database':['postgres']},'byte_budget':1200})
    assert 'No relevant' in c.tool('recall_memory',{'task':'garden flowers','context_features':{'database':['postgres']}})
    feedback={'id':m['id'],'feedback_id':'stable-run','helpful':True}
    c.tool('feedback_memory',feedback);c.tool('feedback_memory',feedback);c.close()
    restarted=Client(args);assert m['id'] in restarted.tool('recall_memory',{'task':'validation evidence','context_features':{'database':['postgres']}});restarted.close()
    assert cli('list')['memories'][0]['helpful']==1
    # Separate MCP clients record concurrently while a real dashboard endpoint polls.
    ui=subprocess.Popen([BINARY,'ui',*args,'--port','7347'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    try:
        for _ in range(100):
            try:
                html=urllib.request.urlopen('http://127.0.0.1:7347/',timeout=1).read().decode();break
            except OSError:time.sleep(.02)
        import re
        token=re.search(r"const TOKEN='([^']+)'",html).group(1)
        def poll():
            for _ in range(12):
                req=urllib.request.Request('http://127.0.0.1:7347/api/dashboard',headers={'Authorization':'Bearer '+token})
                with urllib.request.urlopen(req,timeout=5) as r: assert r.status==200
        def worker(n):
            client=Client(args);ids=[]
            try:
                for i in range(10):
                    row=json.loads(client.tool('record_memory',{'outcome':'good','incident':f'SYNTHETIC worker {n} run {i}','lesson':f'Validate worker {n} run {i}','source':f'SYNTHETIC {n}/{i}'}));ids.append(row['id'])
                client.tool('feedback_memory',feedback)
            finally:client.close()
            return ids
        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:
            jobs=[pool.submit(worker,1),pool.submit(worker,2),pool.submit(poll)]
            ids=jobs[0].result()+jobs[1].result();jobs[2].result()
        allrows=cli('list')['memories'];assert len(allrows)==21 and len(set(ids))==20
        assert next(x for x in allrows if x['id']==m['id'])['helpful']==1
    finally:ui.terminate();ui.wait(timeout=5)
    assert cli('language')['target_percent']==80
    assert cli('language',['--ste-target','100'])['target_percent']==100
    strict=Client(args)
    rejected=strict.call('tools/call',{'name':'record_memory','arguments':{'class':'scar','incident':'SYNTHETIC original evidence','lesson':'Utilize the pool.','source':'SYNTHETIC strict test'}})
    assert rejected['isError'] and 'STE target 100' in rejected['content'][0]['text']
    row=json.loads(strict.tool('record_memory',{'class':'scar','incident':'SYNTHETIC original evidence','lesson':'Bound active queries to the pool limit.','source':'SYNTHETIC strict test'}));strict.close()
    assert row['class']=='scar' and row['outcome']=='worst' and row['incident']=='SYNTHETIC original evidence'
    assert cli('inspect',['--id',row['id']])['writing']['policy']['target_percent']==100
    assert cli('scars')[0]['id']==row['id']
    assert cli('map')['trails']
    assert cli('status')['language']['target_percent']==100
    assert cli('stats')['memories']==22
    print(json.dumps({'version':subprocess.check_output([BINARY,'version'],text=True).strip(),'external_mcp':True,'schema_shape':True,'config_spaces_and_toml':True,'requirements_and_abstention':True,'restart_persistence':True,'concurrent_mcp_clients_and_dashboard':True,'acknowledged_records':22,'strict_language_and_classes':True,'map_and_cli_vocabulary':True,'feedback_dedup':True}))
