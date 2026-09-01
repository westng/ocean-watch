package mcpserver

const listInputSchema = `{
  "type":"object","additionalProperties":false,
  "properties":{
    "channel":{"type":"string","enum":["all","marketing","qianchuan"],"default":"all"},
    "limit":{"type":"integer","minimum":1,"maximum":100,"default":50},
    "cursor":{"type":"string","minLength":1,"maxLength":512}
  }
}`

// objectOneOf opens a two-branch tool output schema. Claude Code's MCP client
// rejects an outputSchema that has no top-level "type", even though the MCP
// spec and the Go SDK both accept a bare "oneOf". Every branch below is already
// an object schema, so declaring the type here adds no constraint and keeps a
// single payload valid on both Codex and Claude Code.
const objectOneOf = `{"type":"object","oneOf":[`

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

const listOutputSchema = objectOneOf +listSuccessSchema + `,` + errorOutputSchema + `]}`

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

const getOutputSchema = objectOneOf +getSuccessSchema + `,` + errorOutputSchema + `]}`

const preflightInputSchema = `{
  "type":"object","additionalProperties":false,"required":["plan_template","items"],
  "properties":{
    "plan_template":{"type":"string","minLength":1,"maxLength":256},
    "items":{"type":"array","minItems":1,"maxItems":100,"items":{"type":"object","additionalProperties":false,"required":["work_url"],"properties":{
      "work_url":{"type":"string","minLength":1,"maxLength":2048},
      "plan_type":{"type":"string","maxLength":128},"business":{"type":"string","maxLength":128}
    }}},
    "concurrency":{"type":"integer","minimum":1,"maximum":10,"default":8},
    "auth_account_id":{"type":"string","maxLength":256}
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
	      "required":["owner_hint_cache","link_metadata","stages","requests"],
	      "properties":{
	        "owner_hint_cache":{"type":"object","additionalProperties":false,
          "required":["supplied","eligible","verified","stale","authorized_hint_query_count","authorized_hint_failure_count","official_video_query_count","loaded","loaded_from_cache","loaded_from_link_metadata","stored"],
          "properties":{
            "supplied":{"type":"integer","minimum":0},"eligible":{"type":"integer","minimum":0},"verified":{"type":"integer","minimum":0},"stale":{"type":"integer","minimum":0},
            "authorized_hint_query_count":{"type":"integer","minimum":0},"authorized_hint_failure_count":{"type":"integer","minimum":0},
            "official_video_query_count":{"type":"integer","minimum":0},"loaded":{"type":"integer","minimum":0},"loaded_from_cache":{"type":"integer","minimum":0},
            "loaded_from_link_metadata":{"type":"integer","minimum":0},"stored":{"type":"integer","minimum":0}
          }
        },
        "link_metadata":{"type":"object","additionalProperties":false,"required":["provider","enabled"],
          "properties":{"provider":{"type":"string","maxLength":128},"enabled":{"type":"boolean"}}},
        "stages":{"type":"object","additionalProperties":false,
          "required":["input_normalization_seconds","link_resolution_seconds","f2_resolution_seconds","credential_resolution_seconds","official_verification_seconds","plan_inventory_seconds","group_reconciliation_seconds","material_diff_seconds","snapshot_persistence_seconds","total_runtime_seconds"],
          "properties":{
            "input_normalization_seconds":{"type":"number","minimum":0},"link_resolution_seconds":{"type":"number","minimum":0},"f2_resolution_seconds":{"type":"number","minimum":0},
            "credential_resolution_seconds":{"type":"number","minimum":0},"official_verification_seconds":{"type":"number","minimum":0},"plan_inventory_seconds":{"type":"number","minimum":0},
            "group_reconciliation_seconds":{"type":"number","minimum":0},"material_diff_seconds":{"type":"number","minimum":0},"snapshot_persistence_seconds":{"type":"number","minimum":0},"total_runtime_seconds":{"type":"number","minimum":0}
          }},
        "requests":{"type":"object","additionalProperties":false,
          "required":["short_link_count","f2_count","official_request_count","retry_count","cache_hit_count","cache_miss_count","binding_hit_count","binding_drift_count","lock_wait_milliseconds"],
          "properties":{
            "short_link_count":{"type":"integer","minimum":0},"f2_count":{"type":"integer","minimum":0},"official_request_count":{"type":"integer","minimum":0},"retry_count":{"type":"integer","minimum":0},
            "cache_hit_count":{"type":"integer","minimum":0},"cache_miss_count":{"type":"integer","minimum":0},"binding_hit_count":{"type":"integer","minimum":0},"binding_drift_count":{"type":"integer","minimum":0},"lock_wait_milliseconds":{"type":"number","minimum":0}
          }}
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
  }
}`

// preflightDefs holds the shared subschemas that preflightSuccessSchema targets
// with "#/$defs/..." references. A "#" reference resolves against the root of
// the whole output schema, not the branch it appears in, so these definitions
// are composed at that root by preflightOutputSchema rather than nested inside
// the success branch.
const preflightDefs = `"$defs":{
    "group":{"type":"object","additionalProperties":false,
        "required":["group_id","creator_id","plan_type","business","product_ids","input_item_ids","already_present_item_ids","completed_item_ids","status"],
      "properties":{
        "group_id":{"type":"string","pattern":"^qcg_[0-9a-f]{64}$"},
        "creator_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},"douyin_id":{"type":"string","maxLength":128},"creator_name":{"type":"string","maxLength":256},
        "plan_type":{"type":"string","maxLength":128},"business":{"type":"string","maxLength":128},"error_code":{"type":"string","pattern":"^[A-Z][A-Z0-9_]{2,63}$"},
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
  }`

const preflightOutputSchema = `{"type":"object",` + preflightDefs + `,"oneOf":[` +
	preflightSuccessSchema + `,` + errorOutputSchema + `]}`

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
      "required":["schema_version","preflight_id","created_at","expires_at","advertiser_id","template_id","template_name","product_name","product_short_name","product_ids","eligible_works","skipped_works","decisions","ready_for_submit"],
      "properties":{
        "schema_version":{"type":"integer","enum":[1,2]},
        "preflight_id":{"type":"string","pattern":"^qianchuan-preflight-[0-9]{8}t[0-9]{6}-[0-9a-f]{12}$"},
        "created_at":{"type":"string","format":"date-time","maxLength":64},"expires_at":{"type":"string","format":"date-time","maxLength":64},
        "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},"template_id":{"type":"string","minLength":1,"maxLength":256},
        "template_name":{"type":"string","maxLength":256},"product_name":{"type":"string","maxLength":256},"product_short_name":{"type":"string","maxLength":256},
        "product_ids":{"type":"array","minItems":1,"maxItems":30,"items":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"}},
        "business_date":{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$"},
        "eligible_works":{"type":"integer","minimum":0},"skipped_works":{"type":"integer","minimum":0},
        "decisions":{"type":"array","minItems":1,"items":{"type":"object","additionalProperties":false,"required":["group_id","creator_id","plan_type","business","action"],
          "properties":{"group_id":{"type":"string","pattern":"^qcg_[0-9a-f]{64}$"},"creator_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"},"plan_type":{"type":"string","maxLength":128},"business":{"type":"string","maxLength":128},"action":{"type":"string","enum":["create","append","would_create","would_append","noop","legacy_binding_required","binding_drift"]},"existing_plan_id":{"type":"string","pattern":"^[1-9][0-9]{0,63}$"}},
          "allOf":[
            {"if":{"properties":{"action":{"const":"append"}}},"then":{"required":["existing_plan_id"]}},
            {"if":{"properties":{"action":{"const":"create"}}},"then":{"not":{"required":["existing_plan_id"]}}}
          ]}},
        "ready_for_submit":{"type":"boolean"}
      }
    }
  }
}`

const getPreflightOutputSchema = objectOneOf +getPreflightSuccessSchema + `,` + errorOutputSchema + `]}`
