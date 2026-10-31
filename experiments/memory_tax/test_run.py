import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import types
import unittest

spec = importlib.util.spec_from_file_location('run', Path(__file__).with_name('run.py'))
r = importlib.util.module_from_spec(spec)
spec.loader.exec_module(r)


class Contracts(unittest.TestCase):
    def setUp(self):
        self.data = r.load_dataset(Path(__file__).with_name('smoke.json'))
        self.case = copy.deepcopy(self.data['cases'][0])

    def test_scope_and_time_before_model(self):
        docs = [d for d in self.case['evidence'] if r.eligible(d, self.case)]
        self.assertEqual([d['id'] for d in docs], ['raw'])
        for scope in ('team', 'conversation'):
            d = dict(docs[0], scope=scope)
            self.assertFalse(r.eligible(d, self.case))
        self.assertFalse(r.eligible(dict(docs[0], available=False), self.case))

    def test_applicability_does_not_hide_source(self):
        case = self.data['cases'][5]
        self.assertFalse(r.eligible(case['lessons'][0], case, True))
        self.assertTrue(r.eligible(case['evidence'][0], case))

    def test_independent_source_without_lesson(self):
        case = self.data['cases'][3]
        args = types.SimpleNamespace(candidates=10, budget=4000)
        result = r.evaluate(case, 'raw_bm25', args, None)
        self.assertTrue(result['retrieval_complete'])

    def test_utf8_budget(self):
        docs = r.chunks([dict(self.case['evidence'][0], kind='evidence', text='தமிழ் ' * 300)])
        context, selected, omitted = r.pack(docs, 700)
        self.assertLessEqual(len(context.encode()), 700)
        self.assertTrue(omitted)

    def test_hash_and_split_validation(self):
        for mutate in ('hash', 'split'):
            data = copy.deepcopy(self.data)
            if mutate == 'hash':
                data['cases'][0]['evidence'][0]['text'] += 'changed'
            else:
                data['cases'][20]['group'] = data['cases'][0]['group']
            with tempfile.TemporaryDirectory() as tmp:
                path = Path(tmp)/'data.json'
                path.write_text(json.dumps(data))
                with self.assertRaises(ValueError):
                    r.load_dataset(path)

    def test_multi_evidence_requires_all(self):
        case = self.data['cases'][2]
        selected = r.chunks([dict(case['evidence'][0], kind='evidence')])
        result = r.measure(case, 'test', '', selected, [], {}, 0, 4000)
        self.assertFalse(result['retrieval_complete'])
        self.assertEqual(result['evidence_recall'], .5)

    def test_hybrid_model_never_receives_forbidden_docs(self):
        class Fake:
            def embed(self, texts, query=False):
                return [[1, 0] for _ in texts]
            def rerank(self, query, docs):
                self.ids = {d['id'] for d in docs}
                return [1. for _ in docs]
        models = Fake()
        args = types.SimpleNamespace(candidates=10, budget=4000, min_rerank=0)
        r.evaluate(self.case, 'combined', args, models)
        self.assertNotIn('future', models.ids)
        self.assertNotIn('other-tenant', models.ids)

    def test_empty_raw_gold_does_not_credit_missing_lesson(self):
        case = copy.deepcopy(self.case)
        case['gold_evidence'] = []
        result = r.measure(case, 'test', '', [], [], {}, 0, 4000)
        self.assertFalse(result['retrieval_complete'])

    def test_gold_fact_in_omitted_tail(self):
        case = copy.deepcopy(self.case)
        d = case['evidence'][0]
        d['text'] = 'noise ' * 300 + 'TAIL_FACT'
        case['gold_spans'] = {'raw': [[1800, 1809]]}
        selected = r.chunks([dict(d, kind='evidence')])[:1]
        result = r.measure(case, 'test', '', selected, [], {}, 0, 4000)
        self.assertEqual(result['evidence_recall'], 0)
        self.assertFalse(result['retrieval_complete'])

    @unittest.skipUnless(os.environ.get('ELEPHANT_EVAL_BINARY'), 'set ELEPHANT_EVAL_BINARY for actual Go integration')
    def test_actual_production_helper(self):
        args = types.SimpleNamespace(elephant_binary=Path(os.environ['ELEPHANT_EVAL_BINARY']),
                                     budget=4000, rounds=1)
        result = r.evaluate(self.case, 'elephant', args, None)
        self.assertIn('lesson', result['selected_ids'])
        self.assertTrue(result['retrieval_complete'])
        self.assertLessEqual(result['context_bytes'], 4000)

    def test_oracle_is_explicit_and_budgeted(self):
        args = types.SimpleNamespace(candidates=10, budget=128)
        out = r.evaluate(self.case, 'oracle_raw', args, None)
        self.assertFalse(out['retrieval_complete'])
        self.assertTrue(out['budget_omissions'])


if __name__ == '__main__':
    unittest.main()
