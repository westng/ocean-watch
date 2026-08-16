package mcpserver

const listInputSchema = `{
  "type":"object","additionalProperties":false,
  "properties":{
    "channel":{"type":"string","enum":["all","marketing","qianchuan"],"default":"all"},
    "limit":{"type":"integer","minimum":1,"maximum":100,"default":50},
    "cursor":{"type":"string","minLength":1,"maxLength":512}
  }
}`

const errorOutputSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","error"],
  "properties":{
    "ok":{"const":false},"request_id":{"type":"string","minLength":1,"maxLength":128},
    "error":{"type":"object","additionalProperties":false,"required":["code","message","retryable","details"],
      "properties":{
        "code":{"type":"string","pattern":"^[A-Z][A-Z0-9_]{2,63}$"},
        "message":{"type":"string","minLength":1,"maxLength":500},
        "retryable":{"type":"boolean"},"details":{"type":"object","maxProperties":20}
      }
    }
  }
}`

const listSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","state_version","source","total_count","items","next_cursor"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},
    "state_version":{"type":"string","minLength":1,"maxLength":256},"source":{"const":"local_state"},
    "total_count":{"type":"integer","minimum":0},
    "items":{"type":"array","maxItems":100,"items":{"type":"object","additionalProperties":false,
      "required":["template_id","channel","template_kind","name","status","advertiser_id","ready_for_plan_creation"],
      "properties":{
        "template_id":{"type":"string","minLength":1,"maxLength":256},
        "channel":{"type":"string","enum":["marketing","qianchuan"]},
        "template_kind":{"type":"string","enum":["marketing","product","live"]},
        "name":{"type":"string","minLength":1,"maxLength":256},
        "status":{"type":["string","null"],"minLength":1,"maxLength":64},
        "advertiser_id":{"type":["string","null"],"maxLength":64},
        "ready_for_plan_creation":{"type":"boolean"}
      }}},
    "next_cursor":{"type":["string","null"],"maxLength":512}
  }
}`

const listOutputSchema = `{"oneOf":[` + listSuccessSchema + `,` + errorOutputSchema + `]}`

const getInputSchema = `{
  "type":"object","additionalProperties":false,"required":["channel","template_id"],
  "properties":{
    "channel":{"type":"string","enum":["marketing","qianchuan"]},
    "template_id":{"type":"string","minLength":1,"maxLength":256}
  }
}`

const getSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","state_version","source","template"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},
    "state_version":{"type":"string","minLength":1,"maxLength":256},"source":{"const":"local_state"},
    "template":{"type":"object","additionalProperties":false,
      "required":["template_id","channel","template_kind","name","status","ready_for_plan_creation","advertiser_id","product_id","product_ids","product_name","creator_name","aweme_id","material_source_type","daily_budget","roi_goal","smart_bid_type","project_name_template","promotion_name_template","validation_issues"],
      "properties":{
        "template_id":{"type":"string","minLength":1,"maxLength":256},
        "channel":{"type":"string","enum":["marketing","qianchuan"]},
        "template_kind":{"type":"string","enum":["marketing","product","live"]},
        "name":{"type":"string","minLength":1,"maxLength":256},
        "status":{"type":["string","null"],"minLength":1,"maxLength":64},
        "ready_for_plan_creation":{"type":"boolean"},
        "advertiser_id":{"type":["string","null"],"maxLength":64},
        "product_id":{"type":["string","null"],"maxLength":64},
        "product_ids":{"type":"array","maxItems":30,"items":{"type":"string","minLength":1,"maxLength":64}},
        "product_name":{"type":["string","null"],"maxLength":256},
        "creator_name":{"type":["string","null"],"maxLength":256},
        "aweme_id":{"type":["string","null"],"maxLength":64},
        "material_source_type":{"type":["string","null"],"maxLength":64},
        "daily_budget":{"type":["number","null"],"minimum":0},
        "roi_goal":{"type":["number","null"],"minimum":0},
        "smart_bid_type":{"type":["string","null"],"maxLength":64},
        "project_name_template":{"type":["string","null"],"maxLength":512},
        "promotion_name_template":{"type":["string","null"],"maxLength":512},
        "validation_issues":{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,"maxLength":500}}
      }
    }
  }
}`

const getOutputSchema = `{"oneOf":[` + getSuccessSchema + `,` + errorOutputSchema + `]}`

const preflightInputSchema = `{
  "type":"object","additionalProperties":false,"required":["plan_template","work_urls"],
  "properties":{
    "plan_template":{"type":"string","minLength":1,"maxLength":256},
    "work_urls":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"string","minLength":1,"maxLength":2048}},
    "concurrency":{"type":"integer","minimum":1,"maximum":10,"default":8},
    "auth_account_id":{"type":"string","maxLength":256},
    "plan_type":{"type":"string","maxLength":128},
    "business":{"type":"string","maxLength":128}
  }
}`

const preflightSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","mode","channel","template","counts","results","skipped","query_failures","failed_results","performance","presentation","preflight_id","expires_at"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},
    "mode":{"const":"dry_run"},"channel":{"const":"qianchuan"},
    "template":{"type":"object","additionalProperties":false,
      "required":["template_id","name","advertiser_id","product_ids"],
      "properties":{
        "template_id":{"type":"string","minLength":1,"maxLength":256},"name":{"type":"string","maxLength":256},
        "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},
        "product_ids":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"}}
      }
    },
    "counts":{"type":"object","maxProperties":20,"propertyNames":{"pattern":"^[a-z][a-z0-9_]{0,63}$"},"additionalProperties":{"type":"integer","minimum":0}},
    "results":{"type":"array","maxItems":100,"items":{"$ref":"#/$defs/group"}},
    "skipped":{"type":"array","maxItems":100,"items":{"type":"object","additionalProperties":false,
      "required":["input_index","reason","message"],"properties":{
        "input_index":{"type":"integer","minimum":0,"maximum":99},
        "aweme_item_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},
        "reason":{"type":"string","minLength":1,"maxLength":128},"message":{"const":"work was skipped during preflight"}
      }}},
    "query_failures":{"type":"array","maxItems":3000,"items":{"type":"object","additionalProperties":false,
      "required":["creator_id","message"],"properties":{
        "creator_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},
        "product_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},"message":{"const":"official query did not complete"}
      }}},
    "failed_results":{"type":"array","maxItems":100,"items":{"$ref":"#/$defs/group"}},
    "performance":{"type":"object","additionalProperties":false,
      "required":["link_resolution_seconds","credential_resolution_seconds","material_resolution_seconds","plan_reconciliation_seconds","total_seconds","owner_hint_cache","link_metadata"],
      "properties":{
        "link_resolution_seconds":{"type":"number","minimum":0},"credential_resolution_seconds":{"type":"number","minimum":0},
        "material_resolution_seconds":{"type":"number","minimum":0},"plan_reconciliation_seconds":{"type":"number","minimum":0},"total_seconds":{"type":"number","minimum":0},
        "owner_hint_cache":{"type":"object","additionalProperties":false,
          "required":["supplied","eligible","verified","stale","broad_scan_work_count","authorized_hint_query_count","authorized_hint_failure_count","official_video_query_count","loaded","loaded_from_cache","loaded_from_link_metadata","stored"],
          "properties":{
            "supplied":{"type":"integer","minimum":0},"eligible":{"type":"integer","minimum":0},"verified":{"type":"integer","minimum":0},"stale":{"type":"integer","minimum":0},
            "broad_scan_work_count":{"type":"integer","minimum":0},"authorized_hint_query_count":{"type":"integer","minimum":0},"authorized_hint_failure_count":{"type":"integer","minimum":0},
            "official_video_query_count":{"type":"integer","minimum":0},"loaded":{"type":"integer","minimum":0},"loaded_from_cache":{"type":"integer","minimum":0},
            "loaded_from_link_metadata":{"type":"integer","minimum":0},"stored":{"type":"integer","minimum":0}
          }
        },
        "link_metadata":{"type":"object","additionalProperties":false,"required":["provider","enabled"],
          "properties":{"provider":{"type":"string","maxLength":128},"enabled":{"type":"boolean"}}}
      }
    },
    "presentation":{"type":"object","additionalProperties":false,
      "required":["format","required","allow_column_omission","allow_column_reordering","columns","rows","required_details","details_outside_table","rendered_markdown"],
      "properties":{
        "format":{"const":"markdown"},"required":{"const":true},"allow_column_omission":{"const":false},"allow_column_reordering":{"const":false},
        "columns":{"type":"array","minItems":5,"maxItems":5,"items":{"$ref":"#/$defs/presentation_column"}},
        "rows":{"type":"array","maxItems":3000,"items":{"type":"object","additionalProperties":false,
          "required":["plan_id","creator_nickname","product_id","material_id","material_title"],"properties":{
            "plan_id":{"type":"string","maxLength":64},"creator_nickname":{"type":"string","maxLength":256},
            "product_id":{"type":"string","maxLength":64},"material_id":{"type":"string","maxLength":64},"material_title":{"type":"string","maxLength":500}
          }}},
        "required_details":{"type":"array","minItems":3,"maxItems":3,"items":{"$ref":"#/$defs/presentation_column"}},
        "details_outside_table":{"type":"array","minItems":3,"maxItems":3,"items":{"type":"string","enum":["skipped","query_failures","failed_results"]}},
        "rendered_markdown":{"type":"string","minLength":1,"maxLength":500000}
      }
    },
    "preflight_id":{"type":"string","maxLength":128,"pattern":"^(?:|qianchuan-preflight-[0-9]{8}t[0-9]{6}-[0-9a-f]{12})$"},
    "expires_at":{"type":"string","maxLength":64}
  },
  "$defs":{
    "group":{"type":"object","additionalProperties":false,
      "required":["creator_id","product_ids","input_item_ids","already_present_item_ids","completed_item_ids","status"],
      "properties":{
        "creator_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},"douyin_id":{"type":"string","maxLength":128},"creator_name":{"type":"string","maxLength":256},
        "existing_plan_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},"plan_name":{"type":"string","maxLength":512},"plan_status":{"type":"string","maxLength":64},
        "product_ids":{"type":"array","maxItems":30,"items":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"}},
        "input_item_ids":{"type":"array","maxItems":100,"items":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"}},
        "already_present_item_ids":{"type":"array","maxItems":100,"items":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"}},
        "completed_item_ids":{"type":"array","maxItems":100,"items":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"}},
        "status":{"type":"string","pattern":"^[a-z][a-z0-9_]{0,63}$"},"error":{"const":"creator preflight did not complete"}
      }
    },
    "presentation_column":{"type":"object","additionalProperties":false,"required":["field","label"],
      "properties":{"field":{"type":"string","enum":["plan_id","creator_nickname","product_id","material_id","material_title","skipped","query_failures","failed_results"]},"label":{"type":"string","minLength":1,"maxLength":32}}}
  }
}`

const preflightOutputSchema = `{"oneOf":[` + preflightSuccessSchema + `,` + errorOutputSchema + `]}`

const getPreflightInputSchema = `{
  "type":"object","additionalProperties":false,"required":["preflight_id"],
  "properties":{"preflight_id":{"type":"string","pattern":"^qianchuan-preflight-[0-9]{8}t[0-9]{6}-[0-9a-f]{12}$"}}
}`

const getPreflightSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","preflight"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},
    "preflight":{"type":"object","additionalProperties":false,
      "required":["preflight_id","created_at","expires_at","advertiser_id","template_id","template_name","product_name","product_short_name","product_ids","eligible_works","skipped_works","decisions","ready_for_submit"],
      "properties":{
        "preflight_id":{"type":"string","pattern":"^qianchuan-preflight-[0-9]{8}t[0-9]{6}-[0-9a-f]{12}$"},
        "created_at":{"type":"string","format":"date-time","maxLength":64},"expires_at":{"type":"string","format":"date-time","maxLength":64},
        "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},"template_id":{"type":"string","minLength":1,"maxLength":256},
        "template_name":{"type":"string","maxLength":256},"product_name":{"type":"string","maxLength":256},"product_short_name":{"type":"string","maxLength":256},
        "product_ids":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"}},
        "eligible_works":{"type":"integer","minimum":1},"skipped_works":{"type":"integer","minimum":0},
        "decisions":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"required":["creator_id","action"],
          "properties":{"creator_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},"action":{"type":"string","enum":["create","append"]},"existing_plan_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"}},
          "allOf":[
            {"if":{"properties":{"action":{"const":"append"}}},"then":{"required":["existing_plan_id"]}},
            {"if":{"properties":{"action":{"const":"create"}}},"then":{"not":{"required":["existing_plan_id"]}}}
          ]}},
        "ready_for_submit":{"const":true}
      }
    }
  }
}`

const getPreflightOutputSchema = `{"oneOf":[` + getPreflightSuccessSchema + `,` + errorOutputSchema + `]}`
