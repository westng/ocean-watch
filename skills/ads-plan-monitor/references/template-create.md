# Structured local Marketing template creation

Resolve the absolute launcher from the loaded Skill directory. Use `./run templates create --input-json JSON`, or `--input-file -` with one JSON object on stdin. Do not automate the terminal wizard with positional answers. `{}` returns missing channel choices; preserve the user's Marketing/Qianchuan choice. WorkBuddy has no CLI template-creation route.

For Marketing, set `channel="marketing"` and supply a `marketing` object. If `marketing.material_source_type` is missing, ask for `ACCOUNT_UPLOAD` or `CREATOR_AUTHORIZED`. Select `source_template="default"` for a requested new skeleton, or an exact existing source from returned `choices.source_template`; Core filters sources by material type.

```json
{"channel":"marketing","source_template":"default","marketing":{"template_name":"示例营销模板","advertiser_id":"1001","platform":"示例平台","traffic_source":"CID","product_id":"2001","product_name":"示例商品","material_source_type":"ACCOUNT_UPLOAD","selection_mode":"MANUAL","max_materials":5,"daily_budget":300,"roi_goal":1.7,"titles":["这是一条示例商品标题"],"landing_page_url":"https://landing.example.test/item","open_url":"exampleapp://item","track_url":"https://track.example.test/view","action_track_url":"https://track.example.test/click"}}
```

IDs are strings and example values are synthetic. Reuse exact user CID links without interpretation. Optional fields include `product_info`, `selling_points`, `gender`, `ages`, `unlimited_materials`, `creator_ids`, `minimum_remaining`, `plan_source`, `project_name_template` and `promotion_name_template`; they use the same domain validation and clone policy as the human wizard. `product_info` uses the established template product-image/selling-point contract. A new product cannot silently inherit the old product's copy or links.

Only ask for returned `missing_fields`. Business input paths such as `marketing.product_id` are filled in the JSON. Readiness paths such as `tracking_urls.track_url` refer to the effective candidate; provide the matching `marketing.track_url` override. Remaining skeleton/configuration fields require the established configuration workflow, not invented IDs or raw configuration edits.

`status=needs_input` never writes. `status=preview` returns the candidate, `state_version`, `preview_digest` and authorization coverage. Show the exact candidate and ask for approval to save locally. Repeat identical JSON with `--submit --expected-revision REV --preview-digest DIGEST` from that preview. Changed inputs or config require a new preview. Only `status=created` with `changed=true` confirms the save; do not automatically retry commits. Template creation does not authorize online plan writes, which remain unavailable for Marketing in this alpha.
