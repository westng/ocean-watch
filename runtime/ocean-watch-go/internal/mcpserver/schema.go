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
