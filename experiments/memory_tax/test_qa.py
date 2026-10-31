import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest.mock import patch


def module(name):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(name+'.py'))
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value

r, qa = module('run'), module('qa')


class QAContracts(unittest.TestCase):
    def setUp(self):
        self.case = r.load_dataset(Path(__file__).with_name('smoke.json'))['cases'][2]
        self.args = types.SimpleNamespace(budget=2000, candidates=10, min_rerank=0,
                                          rounds=2, qa_command=['adapter'], score_command=None)

    def test_no_gold_and_repeated_context_charged(self):
        requests = []
        def adapter(argv, request):
            requests.append(request)
            self.assertNotIn('gold_evidence', request)
            self.assertNotIn('case', request)
            return dict(model_id='test-only', answer='fixture answer',
                        followup_query='PIN_VERSION' if request['round'] == 1 else None), 1
        row = r.evaluate(self.case, 'raw_bm25', self.args, None)
        with patch.object(qa, 'call', adapter):
            result = qa.answer(row, self.case, self.args, None, r)
        self.assertEqual(len(requests), 2)
        self.assertLessEqual(result['model_context_exposure_bytes'], self.args.budget)
        self.assertEqual(result['model_context_exposure_bytes'], sum(len(x['context'].encode()) for x in requests))
        self.assertIsNone(result['task_success'])
        self.assertIsNone(result['provider_tokens'])

    def test_forbidden_followup_rejected(self):
        row = r.evaluate(self.case, 'none', self.args, None)
        with patch.object(qa, 'call', return_value=({'model_id':'stub', 'answer':'', 'followup_query':'x'}, 1)):
            with self.assertRaises(ValueError):
                qa.answer(row, self.case, self.args, None, r)

    def test_real_subprocess_protocol(self):
        with tempfile.TemporaryDirectory() as tmp:
            adapter = Path(tmp)/'adapter.py'
            adapter.write_text("import json,sys\np=json.load(sys.stdin)\nassert 'gold_evidence' not in p\nprint(json.dumps({'model_id':'protocol-test-only', 'answer':'ok'}))\n")
            response, elapsed = qa.call([sys.executable, str(adapter)], {'operation':'answer', 'query':'test', 'context':''})
            self.assertEqual(response['answer'], 'ok')
            self.assertGreater(elapsed, 0)


if __name__ == '__main__':
    unittest.main()
