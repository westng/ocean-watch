#!/usr/bin/env python3
"""Capture public API contract facts only; never load OAuth or business state.

The endpoint and public library identifier come from the documentation site's
own client. The identifier selects public documentation; it is not a credential.
Examples, headers and raw HTML are deliberately excluded from the saved facts.
"""
import argparse
from concurrent.futures import ThreadPoolExecutor
import hashlib
from html.parser import HTMLParser
import json
from pathlib import Path
import re
import urllib.parse
import urllib.request


class Node:
    def __init__(self, tag, attributes):
        self.tag, self.attributes, self.children = tag, dict(attributes), []

    def text(self):
        return " ".join(child.text() if isinstance(child, Node) else child
                        for child in self.children).strip()

    def walk(self):
        yield self
        for child in self.children:
            if isinstance(child, Node):
                yield from child.walk()


class DocumentParser(HTMLParser):
    def __init__(self):
        super().__init__()
        self.root = Node("root", [])
        self.stack = [self.root]

    def handle_starttag(self, tag, attributes):
        node = Node(tag, attributes)
        self.stack[-1].children.append(node)
        if tag not in {"br", "img", "input", "hr", "meta", "link", "source", "wbr"}:
            self.stack.append(node)

    def handle_endtag(self, tag):
        for index in range(len(self.stack) - 1, 0, -1):
            if self.stack[index].tag == tag:
                self.stack = self.stack[:index]
                break

    def handle_data(self, data):
        self.stack[-1].children.append(data)


def parse_contract(document):
    parser = DocumentParser()
    parser.feed(document.get("content", ""))
    facts = {"title": document.get("title", ""), "api_path": document.get("path", ""),
             "method": "", "request_fields": [], "response_fields": [], "notes": []}
    heading = ""
    for node in parser.root.walk():
        if not isinstance(node, Node):
            continue
        if re.fullmatch(r"h[1-6]", node.tag):
            heading = node.text()
        elif heading == "请求方法" and not facts["method"]:
            method = node.text().strip()
            if method in {"GET", "POST", "PUT", "DELETE", "PATCH"}:
                facts["method"] = method
        elif node.tag == "p" and not heading:
            text = node.text()
            if text:
                facts["notes"].append(text[:3000])
    method = re.search(r"请求(?:方法|方式)\s*[:：]?\s*(GET|POST|PUT|DELETE|PATCH)\b", parser.root.text())
    if method:
        facts["method"] = method.group(1)
    for table in parser.root.walk():
        kind = table.attributes.get("data-tabletype")
        if kind not in {"request", "response"}:
            continue
        ancestors = []
        for row in table.walk():
            if "body" not in row.attributes.get("class", "").split() or "row" not in row.attributes.get("class", "").split():
                continue
            name = next((item.text() for item in row.walk()
                         if item.attributes.get("class") == "col_params_name"), "")
            datatype = next((item.text() for item in row.walk()
                             if "col_param_type" in item.attributes.get("class", "")), "")
            description = next((item.text() for item in row.walk()
                                if "description" in item.attributes.get("class", "").split()), "")
            if not name:
                continue
            level = int(row.attributes.get("data-level", "0"))
            ancestors = ancestors[:level]
            field_path = ".".join(ancestors + [name])
            ancestors.append(name)
            facts[kind + "_fields"].append({"path": field_path, "name": name,
                "type": datatype, "description": description,
                "required": any(item.text() == "必填" for item in row.walk()
                                if "param-tag" in item.attributes.get("class", ""))})
    for table in parser.root.walk():
        if table.tag != "table":
            continue
        rows = [node for node in table.walk() if node.tag == "tr"]
        if not rows:
            continue
        header = rows[0].text()
        if "字段" not in header or "类型" not in header or "描述" not in header:
            continue
        fields = []
        ancestors = []
        for row in rows[1:]:
            cells = [node for node in row.children if isinstance(node, Node) and node.tag in {"td", "th"}]
            if len(cells) < 3:
                continue
            match = re.search(r"[A-Za-z_][A-Za-z_0-9-]*", cells[0].text())
            if not match:
                continue
            name = match.group(0)
            level = next((int(node.attributes["data-level"]) for node in cells[0].walk()
                          if "data-level" in node.attributes), 0)
            ancestors = ancestors[:level]
            fields.append({"path": ".".join(ancestors + [name]), "name": name,
                           "type": cells[1].text(), "description": cells[2].text(),
                           "required": "必填" in cells[0].text()})
            ancestors.append(name)
        names = {field["name"] for field in fields}
        if {"code", "message"}.issubset(names) and ("data" in names or "date" in names):
            facts["response_fields"].extend(fields)
        elif "Access-Token" not in names and "Access-token" not in names:
            facts["request_fields"].extend(fields)
    return facts


def get_json(url):
    request = urllib.request.Request(url, headers={"User-Agent": "OceanWatch-public-contract-review/1"})
    with urllib.request.urlopen(request, timeout=25) as response:
        payload = response.read(4 * 1024 * 1024 + 1)
    if len(payload) > 4 * 1024 * 1024:
        raise ValueError("public document exceeds capture budget")
    result = json.loads(payload)
    if result.get("code") != 0:
        raise ValueError("public documentation unavailable")
    return result["data"]


def walk_json(value):
    if isinstance(value, dict):
        yield value
        for child in value.values():
            yield from walk_json(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk_json(child)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--catalog", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--query-id", action="append", default=[])
    args = parser.parse_args()
    catalog = json.loads(args.catalog.read_text())
    base = "https://open.oceanengine.com/skiff/api/doc/client/"
    labels = get_json(base + "label/tree/get/")
    identifier = next(item["identify_key"] for item in walk_json(labels) if item.get("id") == 12)

    def capture(candidate):
        query_id = candidate["query_id"]
        doc_url = candidate["documentation_url"]
        if query_id == "qianchuan_uni_promotion_list_v1":
            doc_url = "https://open.oceanengine.com/labels/12/docs/1771195810853899"
        doc_id = doc_url.rstrip("/").split("/")[-1]
        try:
            url = base + "node/get/?" + urllib.parse.urlencode(
                {"doc_id": doc_id, "identify_key": identifier, "language": "CHINESE"})
            document = get_json(url)
            facts = parse_contract(document)
            facts.update(query_id=query_id, documentation_url=doc_url,
                         published_api_path=candidate["api_path"], observed_on="2026-11-01")
            canonical = json.dumps(facts, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
            facts["document_digest"] = hashlib.sha256(canonical.encode()).hexdigest()
            facts["capture_status"] = "captured"
            return facts
        except Exception:
            return {"query_id": query_id, "documentation_url": doc_url,
                    "observed_on": "2026-11-01", "capture_status": "unavailable"}

    selected = [query for query in catalog["queries"] if not args.query_id or query["query_id"] in args.query_id]
    with ThreadPoolExecutor(max_workers=2) as executor:
        documents = list(executor.map(capture, selected))
    if args.query_id and args.out.exists():
        previous = json.loads(args.out.read_text())["documents"]
        replacement = {document["query_id"]: document for document in documents}
        documents = [replacement.get(document["query_id"], document) for document in previous]
    output = {"catalog_version": catalog["catalog_version"], "observed_on": "2026-11-01",
              "source": "official_public_api_documentation", "documents": documents}
    args.out.write_text(json.dumps(output, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps({"documents": len(documents),
                      "captured": sum(d.get("capture_status") == "captured" for d in documents),
                      "get_methods": sum(d.get("method") == "GET" for d in documents),
                      "paths_match": sum(d.get("api_path") == d.get("published_api_path") for d in documents),
                      "with_output_fields": sum(bool(d.get("response_fields")) for d in documents)}))


if __name__ == "__main__":
    main()
