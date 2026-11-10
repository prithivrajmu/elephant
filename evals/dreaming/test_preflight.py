import copy
import hashlib
from pathlib import Path
import tempfile
import unittest
from preflight import load_json, validate


class PreflightTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.content = 'Capacity is 8; preserve the exception.\n'.encode()
        (self.root/'source.md').write_bytes(self.content)
        digest = hashlib.sha256(self.content).hexdigest()
        self.manifest = {"schema_version": 1, "stage": "development", "cases": [{
            "id": "case", "incident_family": "family", "split": "development",
            "sources": [{"id": "source", "path": "source.md", "sha256": digest,
                         "start_byte": 0, "end_byte": len(self.content), "span_sha256": digest}]}]}

    def test_source_and_span_identity(self):
        self.assertTrue(validate(self.manifest, self.root)['preflight_passed'])
        for key, value in [('sha256', '0'*64), ('span_sha256', '0'*64),
                           ('start_byte', True), ('end_byte', 10000), ('path', '../escape')]:
            with self.subTest(key=key):
                m = copy.deepcopy(self.manifest)
                m['cases'][0]['sources'][0][key] = value
                with self.assertRaises(ValueError): validate(m, self.root)
        (self.root/'source.md').write_bytes(b'Changed source')
        with self.assertRaises(ValueError): validate(self.manifest, self.root)

    def test_copying_case_cannot_create_holdout(self):
        for new_family in (False, True):
            m = copy.deepcopy(self.manifest)
            other = copy.deepcopy(m['cases'][0]); other['id']='other'; other['split']='holdout'
            if new_family: other['incident_family']='different-declared-family'
            m['cases'].append(other)
            with self.assertRaises(ValueError): validate(m, self.root)

    def test_pilot_design_and_sealed_holdout(self):
        m = copy.deepcopy(self.manifest)
        m['stage'] = 'pilot'
        m['cases'][0]['split'] = 'pilot'
        m['design'] = {k: 'explicit-development-setting' for k in
                       ('reader_model', 'reviewer_model', 'primary_endpoint',
                        'primary_comparison', 'analysis', 'stopping_rule', 'resource_budget')}
        m['design'].update(git_commit='a'*40, seeds=[1, 2])
        self.assertTrue(validate(m, self.root)['design_fields_present'])
        m['cases'][0]['split'] = 'holdout'
        with self.assertRaises(ValueError): validate(m, self.root)
        m['stage'] = 'confirmatory'
        self.assertTrue(validate(m, self.root)['preflight_passed'])
        m['design']['reader_model'] = 'TBD'
        with self.assertRaises(ValueError): validate(m, self.root)

    def test_design_and_json_guards(self):
        m = copy.deepcopy(self.manifest); m['stage']='confirmatory'
        with self.assertRaises(ValueError): validate(m, self.root)
        p = self.root/'duplicate.json'; p.write_text('{"schema_version":1,"schema_version":2}')
        with self.assertRaises(ValueError): load_json(p)
        m = copy.deepcopy(self.manifest); m['cases'].append(m['cases'][0])
        with self.assertRaises(ValueError): validate(m, self.root)


if __name__ == '__main__': unittest.main()
