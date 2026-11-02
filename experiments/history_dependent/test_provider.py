import unittest
from unittest.mock import patch
import openai_adapter as a


class ProviderContracts(unittest.TestCase):
    def setUp(self):
        self.payload=dict(model_id='snapshot-fixture',model_settings={'max_output_tokens':512},task='task',files={},context='',instructions='repair')
        self.pricing=dict(input_usd_per_million=2,cached_input_usd_per_million=1,output_usd_per_million=4)
        self.response=dict(id='response-fixture',model='snapshot-fixture',status='completed',usage={'input_tokens':100,'output_tokens':20,'input_tokens_details':{'cached_tokens':50}},output=[{'type':'message','content':[{'type':'output_text','text':'{"files":{"policy.py":"def contract(): return {}"}}'}]}])

    def test_fresh_stateless_request(self):
        body=a.request_body(self.payload)
        self.assertFalse(body['store']);self.assertEqual(body['tools'],[])
        self.assertNotIn('previous_response_id',body);self.assertNotIn('conversation',body)
        with self.assertRaises(ValueError):a.request_body(dict(self.payload,model_settings={'tools':['web']}))

    def test_usage_estimates_are_not_billed_cost(self):
        result=a.normalize(self.response,self.payload,self.pricing)
        self.assertIsNone(result['billed_cost_usd'])
        self.assertAlmostEqual(result['estimated_cost_usd'],.00023)
        with self.assertRaises(ValueError):a.normalize(dict(self.response,model='alias'),self.payload,self.pricing)

    def test_malformed_model_answer_retains_cost_and_scores_failure(self):
        self.response['output']=[]
        result=a.normalize(self.response,self.payload,self.pricing)
        self.assertEqual(result['files'],{});self.assertEqual(result['usage']['output_tokens'],20)

    def test_truncated_or_deep_output_is_scored_failure(self):
        result=a.normalize(dict(self.response,status='incomplete',incomplete_details={'reason':'max_output_tokens'}),self.payload,self.pricing)
        self.assertEqual(result['files'],{})
        self.response['output'][0]['content'][0]['text']='['*10000+'0'+']'*10000
        self.assertEqual(a.normalize(self.response,self.payload,self.pricing)['files'],{})
        with self.assertRaises(ValueError):a.normalize(dict(self.response,id=None),self.payload,self.pricing)
        self.response['output'][0]['content'][0]['text']=__import__('json').dumps({'files':{'policy.py':'x'*5000}})
        self.assertEqual(a.normalize(self.response,self.payload,self.pricing)['files'],{})

    def test_missing_key_never_makes_network_call(self):
        with patch.dict('os.environ',{},clear=True),patch.object(a.urllib.request,'build_opener') as network:
            with self.assertRaisesRegex(ValueError,'not configured'):a.call(self.payload,self.pricing)
            network.assert_not_called()
        with patch.dict('os.environ',{'OPENAI_API_KEY':'test-only-not-a-key'}),patch.object(a.urllib.request,'build_opener') as network:
            with self.assertRaises(ValueError):a.call(self.payload,{})
            network.assert_not_called()


if __name__=='__main__':unittest.main()
