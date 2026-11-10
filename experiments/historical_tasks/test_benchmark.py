import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('benchmark', Path(__file__).with_name('benchmark.py'))
b = importlib.util.module_from_spec(spec)
spec.loader.exec_module(b)


class HistoricalContracts(unittest.TestCase):
    def test_source_revisions_precede_target(self):
        for task in b.tasks():
            case = b.corpus(task)
            self.assertTrue(case['evidence'])
            for doc in case['evidence']:
                self.assertNotEqual(doc['source_revision'],task['fix'])
                b.git('merge-base','--is-ancestor',doc['source_revision'],task['base'])
                self.assertEqual(b.sha(doc['text'].encode()),doc['sha256'])
            sources={d['id'] for d in case['evidence']}
            self.assertTrue(all(set(d['evidence_ids']) <= sources for d in case['lessons']))

    def test_snapshot_has_no_git_or_future_overlay(self):
        task=b.tasks()[0]
        with tempfile.TemporaryDirectory() as tmp:
            dest=Path(tmp)/'snapshot'
            b.snapshot(task['base'],dest)
            self.assertFalse((dest/'.git').exists())
            self.assertFalse((dest/'cmd/elephant/update_test.go').exists())
            self.assertFalse((dest/'experiments/historical_tasks').exists())

    def test_forbidden_test_mutation_rejected_before_execution(self):
        task={'allowed_changes':['source.go']}
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)
            (root/'source.go').write_text('source')
            (root/'source_test.go').write_text('test')
            before=b.inventory(root)
            (root/'source_test.go').write_text('disabled test')
            with patch.object(b,'run_tests',side_effect=AssertionError('must not run')):
                result=b.score(task,root,before,Path('/no/go'))
            self.assertFalse(result['valid_submission'])
            self.assertEqual(result['forbidden_changes'],['source_test.go'])

    def test_candidate_symlink_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            (Path(tmp)/'link').symlink_to('/etc/hosts')
            with self.assertRaises(ValueError):
                b.inventory(Path(tmp))

    def test_prompt_omits_fix_and_gold(self):
        task=b.tasks()[0]
        with tempfile.TemporaryDirectory() as tmp:
            prompt=b.prepare(task,'none',Path(tmp)/'repo','',Path('/go'))
            self.assertNotIn(task['fix'],prompt)
            self.assertNotIn(task['regression_regex'],prompt)
            self.assertNotIn('oracle',prompt)

    def test_scoring_rejects_source_drift_and_unknown_trial(self):
        with tempfile.TemporaryDirectory() as tmp:
            manifest = {'source_hashes': b.source_hashes(), 'tasks': [], 'trials': {}}
            path = Path(tmp)/'manifest.json'
            path.write_text(json.dumps(manifest))
            argv = ['benchmark.py','score','--state',tmp,'--go','/no/go','--trial','missing']
            with patch('sys.argv',argv), patch.object(b,'tasks',return_value=[]):
                with self.assertRaisesRegex(ValueError,'unknown trial ID'):
                    b.main()
                manifest['source_hashes'] = {}
                path.write_text(json.dumps(manifest))
                with self.assertRaisesRegex(ValueError,'sources changed'):
                    b.main()


if __name__=='__main__':
    unittest.main()
