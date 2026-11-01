import json
import unittest

from scripts.capture_official_query_docs import parse_contract


class PublicContractParserTests(unittest.TestCase):
    def test_nested_contract_tables_exclude_examples(self):
        content = '''<p>公开接口说明</p><h2>请求方法</h2><p>GET</p>
        <div data-tabletype="request">
          <div class="body row" data-level="0"><span class="col_params_name">filtering</span><span class="col_param_type">object</span><span class="param-tag">必填</span></div>
          <div class="body row" data-level="1"><span class="col_params_name">tab</span><span class="col_param_type">string</span><span class="description">商品分类</span></div>
          <div class="body row" data-level="0"><span class="col_params_name">page</span><span class="col_param_type">number</span></div>
        </div><div data-tabletype="response">
          <div class="body row" data-level="0"><span class="col_params_name">data</span><span class="col_param_type">object</span></div>
          <div class="body row" data-level="1"><span class="col_params_name">list</span><span class="col_param_type">object[]</span></div>
          <div class="body row" data-level="2"><span class="col_params_name">id</span><span class="col_param_type">number</span><span class="description">商品 ID</span></div>
        </div><h2>请求示例</h2><pre>{"access_token":"synthetic-example-secret"}</pre>'''
        facts = parse_contract({"title": "合成文档", "path": "/open_api/v1.0/qianchuan/product/get/", "content": content})
        self.assertEqual(facts["method"], "GET")
        self.assertEqual([f["path"] for f in facts["request_fields"]], ["filtering", "filtering.tab", "page"])
        self.assertTrue(facts["request_fields"][0]["required"])
        self.assertEqual([f["path"] for f in facts["response_fields"]], ["data", "data.list", "data.list.id"])
        self.assertNotIn("synthetic-example-secret", json.dumps(facts))

    def test_legacy_tables_preserve_source_root_and_amount_unit(self):
        content = '''<h3>请求方式</h3><p>GET</p><table><tr><th>字段</th><th>类型</th><th>描述</th></tr>
        <tr><td>advertiser_id 必填</td><td>number</td><td>客户 ID</td></tr></table>
        <table><tr><th>字段</th><th>类型</th><th>描述</th></tr>
        <tr><td>code</td><td>number</td><td>业务码</td></tr>
        <tr><td>message</td><td>string</td><td>状态</td></tr>
        <tr><td>date</td><td>object</td><td>公开原文拼写</td></tr>
        <tr><td><span data-level="1">balance</span></td><td>number</td><td>单位为千分之一分</td></tr></table>'''
        facts = parse_contract({"content": content})
        self.assertEqual(facts["method"], "GET")
        self.assertEqual(facts["request_fields"][0]["path"], "advertiser_id")
        self.assertEqual(facts["response_fields"][-1]["path"], "date.balance")
        self.assertEqual(facts["response_fields"][-1]["description"], "单位为千分之一分")


if __name__ == "__main__":
    unittest.main()
