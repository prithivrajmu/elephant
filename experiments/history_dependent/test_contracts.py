import copy
import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

HERE=Path(__file__).resolve().parent
sys.path.insert(0,str(HERE))
import fixtures
import runner
import retrieval
import calibrate


class Contracts(unittest.TestCase):
    def setUp(self):self.data=fixtures.make();self.case=self.data['cases'][0]

    def test_generation_and_split_provenance(self):
        self.assertEqual(self.data,json.loads((HERE/'fixtures.json').read_text()))
        dev={c['group'] for c in self.data['cases'] if c['split']=='dev'}
        reserved={c['group'] for c in self.data['cases'] if c['split']=='reserved'}
        self.assertFalse(dev & reserved);self.assertEqual(len(reserved),24)
        for c in self.data['cases']:
            self.assertNotIn(json.dumps(c['expected'],sort_keys=True),c['query'])
            self.assertNotIn(repr(c['expected']),c['files']['policy.py'])
        self.assertTrue(runner.controls(self.data)['valid'])

    def test_generated_code_never_executes(self):
        expected=self.case['expected']
        self.assertTrue(runner.evaluate({'policy.py':'def contract():\n return '+repr(expected)},expected))
        for text in ["import os\ndef contract(): return {}", "def contract():\n return __import__('os').system('false')", "@print('oops')\ndef contract(): return {}", 'def contract(x=print("oops")): return {}']:
            self.assertFalse(runner.evaluate({'policy.py':text},expected))
        with self.assertRaises(ValueError):runner.evaluate({'../policy.py':'x'},expected)
        with self.assertRaises(ValueError):runner.evaluate({'policy.py':'x'*4097},expected)
        wrong=dict(expected,retries=True)
        self.assertFalse(runner.evaluate({'policy.py':'def contract():\n return '+repr(wrong)},expected))

    def test_dedup_preserves_disjoint_source_chunks(self):
        raw=dict(id='raw',kind='evidence',span=[0,10]);other=dict(raw,span=[10,20])
        lesson=dict(id='lesson',kind='lesson',evidence_ids=['raw'])
        self.assertEqual(retrieval.deduplicate([raw,other,lesson]),[raw,other])
        self.assertEqual(retrieval.deduplicate([lesson,raw]),[lesson])
        self.assertEqual(retrieval.deduplicate([raw,dict(raw,span=[5,15])]),[raw])

    def test_prepare_binds_controls_and_calibration(self):
        with tempfile.TemporaryDirectory() as d:
            helper=Path(d)/'helper';helper.write_text('fixture')
            lock={'runtime_files':[sys.executable],'model_settings':{'temperature':0}}
            control=runner.controls(self.data)
            chosen=dict(short_query=True,threshold=-2,deduplicate=True)
            cal=dict(binding=runner.binding(),split='dev',case_ids=[c['id'] for c in self.data['cases'] if c['split']=='dev'],selected=chosen,candidates=[chosen])
            m=runner.prepare(self.data,control,cal,helper,'model-snapshot',[sys.executable],3,lock)
            self.assertEqual(len(m['cells']),24*5*3)
            self.assertTrue(all(c['case'].startswith('reserved') for c in m['cells']))
            already_fixed=copy.deepcopy(self.data)
            already_fixed['cases'][0]['files']['policy.py']='def contract():\n return '+repr(already_fixed['cases'][0]['expected'])
            with self.assertRaises(ValueError):runner.prepare(already_fixed,runner.controls(already_fixed),cal,helper,'model-snapshot',[sys.executable],3,lock)
            control['binding']='wrong'
            with self.assertRaises(ValueError):runner.prepare(self.data,control,cal,helper,'model-snapshot',[sys.executable],3,lock)
            control=runner.controls(self.data);control['rows']=[]
            with self.assertRaises(ValueError):runner.prepare(self.data,control,cal,helper,'model-snapshot',[sys.executable],3,lock)
            control=runner.controls(self.data);cal['case_ids'].append('reserved-route-00')
            with self.assertRaises(ValueError):runner.prepare(self.data,control,cal,helper,'model-snapshot',[sys.executable],3,lock)
            cal['split']='reserved'
            with self.assertRaises(ValueError):runner.prepare(self.data,control,cal,helper,'model-snapshot',[sys.executable],3,lock)

    def test_calibration_never_reads_reserved_cases(self):
        class Models:
            def manifest(self):return {}
        data=copy.deepcopy(self.data)
        for c in data['cases']:
            if c['split']=='reserved':c.clear();c['split']='reserved'
        def ranking(q,docs,models,k,minimum,trace):
            trace['reranker_scores']={d['chunk_id']:1 for d in docs};return docs
        with patch.object(retrieval.engine,'hybrid',side_effect=ranking):result=calibrate.calibrate(data,Models())
        self.assertEqual(len(result['case_ids']),12)
        self.assertTrue(all(x.startswith('dev') for x in result['case_ids']))

    def test_adapter_pinning_and_usage_required(self):
        response=dict(model_id='pinned',files={'policy.py':'def contract(): return {}'},usage={'input_tokens':10,'output_tokens':5},billed_cost_usd=.01)
        with tempfile.TemporaryDirectory() as d:
            p=Path(d)/'adapter.py'
            def call(value):
                p.write_text('print('+repr(json.dumps(value))+')')
                return runner.adapter_call([sys.executable,str(p)],{'operation':'repair'},'pinned')
            self.assertEqual(call(response)['usage']['input_tokens'],10)
            for bad in [dict(response,model_id='alias'),dict(response,usage={}),dict(response,billed_cost_usd=None),dict(response,billed_cost_usd=float('nan'))]:
                with self.assertRaises(ValueError):call(bad)

    def test_repeated_run_payload_and_artifact_tampering(self):
        data=copy.deepcopy(self.data)
        data['cases']=[c for c in data['cases'] if c['split']=='dev']+[next(c for c in data['cases'] if c['split']=='reserved')]
        expected=data['cases'][-1]['expected']
        with tempfile.TemporaryDirectory() as d:
            root=Path(d);helper=root/'helper';helper.write_text('fixture')
            adapter=root/'adapter.py'
            response=dict(model_id='fixture-pinned',model_settings={'temperature':0},files={'policy.py':'def contract():\n return '+repr(expected)},usage={'input_tokens':10,'output_tokens':5},billed_cost_usd=0)
            adapter.write_text('import json,sys\np=json.load(sys.stdin)\nassert "expected" not in p and "gold_evidence" not in p\nprint('+repr(json.dumps(response))+')')
            chosen=dict(short_query=False,threshold=0,deduplicate=True)
            calibration=dict(binding=runner.binding(),split='dev',case_ids=[c['id'] for c in data['cases'] if c['split']=='dev'],selected=chosen,candidates=[chosen])
            lock={'runtime_files':[str(adapter)],'model_settings':{'temperature':0}}
            m=runner.prepare(data,runner.controls(data),calibration,helper,'fixture-pinned',[sys.executable,str(adapter)],3,lock)
            m.update(model_cache=str(root),models={})
            class Models:
                def __init__(self,*a):pass
                def manifest(self):return {}
            with patch.object(runner.engine,'Models',Models),patch.object(runner,'context',return_value=('',[])):
                runner.run(data,m,root/'output')
            rows=[json.loads(p.read_text()) for p in (root/'output').glob('[0-9]*.json')]
            self.assertEqual(len(rows),15);self.assertTrue(all(r['success'] for r in rows))
            self.assertEqual(json.loads((root/'output/status.json').read_text())['status'],'complete')
            limited=copy.deepcopy(m);limited['max_estimated_usd']=.001
            with patch.object(runner.engine,'Models',Models),patch.object(runner,'context',return_value=('',[])),patch.object(runner,'adapter_call',return_value=dict(response,billed_cost_usd=.002)):
                runner.run(data,limited,root/'limited')
            status=json.loads((root/'limited/status.json').read_text())
            self.assertEqual(status['status'],'cost_limit_reached');self.assertEqual(status['completed'],1)
            bad=copy.deepcopy(m);bad['calibration']['selected']['threshold']=-5
            with self.assertRaisesRegex(ValueError,'artifacts changed'):runner.run(data,bad,root/'bad')
            adapter.write_text('changed')
            with self.assertRaisesRegex(ValueError,'adapter changed'):runner.run(data,m,root/'again')


if __name__=='__main__':unittest.main()
