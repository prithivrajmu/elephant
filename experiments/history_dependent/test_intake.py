import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import history_intake as h
import setup_provider as setup


class IntakeContracts(unittest.TestCase):
    def setUp(self):
        self.data=dict(classification='authentic_private',redacted=True,approved_for_external_model=False,tasks=[dict(id='t1',group='g1',split='dev',base_revision='a'*40,fix_revision='b'*40,allowed_changes=['policy.py'],test_overlay=['policy_test.py'],prompt='Restore the earlier decision',cutoff='2026-10-01T00:00:00Z',history=[dict(id='h1',at='2026-09-01T00:00:00Z',role='user',text='Earlier decision')])])

    def test_private_intake_not_approval_to_send(self):
        result=h.validate(self.data);self.assertEqual(result['turns'],1);self.assertFalse(result['approved_for_external_model'])

    def test_cutoff_split_and_path_rejection(self):
        bad=copy.deepcopy(self.data);bad['tasks'][0]['history'][0]['at']='2026-10-01T00:00:00Z'
        with self.assertRaises(ValueError):h.validate(bad)
        bad=copy.deepcopy(self.data);bad['tasks'][0]['allowed_changes']=['../secret']
        with self.assertRaises(ValueError):h.validate(bad)
        bad=copy.deepcopy(self.data);extra=copy.deepcopy(bad['tasks'][0]);extra.update(id='t2',split='reserved');bad['tasks'].append(extra)
        with self.assertRaises(ValueError):h.validate(bad)

    def test_setup_missing_key_has_no_side_effects(self):
        with tempfile.TemporaryDirectory() as d,patch.dict('os.environ',{},clear=True),patch.object(setup.subprocess,'run') as run:
            state=Path(d)/'new'
            with self.assertRaises(ValueError):setup.configure(state,'snapshot',Path('/pricing'),Path('/helper'),Path('/cache'),{})
            self.assertFalse(state.exists());run.assert_not_called()


if __name__=='__main__':unittest.main()
