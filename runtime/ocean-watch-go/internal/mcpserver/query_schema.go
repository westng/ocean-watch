package mcpserver

const managedAccountsInputSchema = `{
  "type":"object","additionalProperties":false,
  "properties":{
    "channel":{"type":"string","enum":["all","marketing","qianchuan"],"default":"all"},
    "include_disabled":{"type":"boolean","default":false}
  }
}`

const managedAccountsSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","total_count","accounts","presentation"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"local_state"},
    "total_count":{"type":"integer","minimum":0},
    "accounts":{"type":"array","maxItems":10000,"items":{"type":"object","additionalProperties":false,
      "required":["channel","name","advertiser_id","enabled"],"properties":{
        "channel":{"type":"string","enum":["marketing","qianchuan"]},"name":{"type":"string","minLength":1,"maxLength":100},
        "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"enabled":{"type":"boolean"}
      }}},
    "presentation":{"type":"object","additionalProperties":false,"required":["required","rendered_markdown"],"properties":{
      "required":{"type":"boolean"},"rendered_markdown":{"type":"string","minLength":1,"maxLength":2000000}
    }}
  }
}`

const managedAccountsOutputSchema = objectOneOf +managedAccountsSuccessSchema + `,` + errorOutputSchema + `]}`

const qianchuanAuthorizationInputSchema = `{
  "type":"object","additionalProperties":false,
  "properties":{"advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"}}
}`

const qianchuanAuthorizationSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","status","mappings","authorizations"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"local_state"},
    "status":{"type":"object","additionalProperties":false,
      "required":["has_app_id","has_secret","authorization_count","authorized_account_count","authorized_advertiser_count","generation","advertiser_id","advertiser_id_authorized"],
      "properties":{
        "has_app_id":{"type":"boolean"},"has_secret":{"type":"boolean"},"authorization_count":{"type":"integer","minimum":0},
        "authorized_account_count":{"type":"integer","minimum":0},"authorized_advertiser_count":{"type":"integer","minimum":0},
        "generation":{"type":"integer","minimum":0},"advertiser_id":{"type":["string","null"],"pattern":"^[1-9][0-9]{0,18}$"},
        "advertiser_id_authorized":{"type":["boolean","null"]}
      }},
    "mappings":{"type":"array","maxItems":100000,"items":{"type":"object","additionalProperties":false,
      "required":["advertiser_id","authorization_ids","ambiguous"],"properties":{
        "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
        "authorization_ids":{"type":"array","maxItems":1000,"items":{"type":"string","minLength":1,"maxLength":256}},
        "ambiguous":{"type":"boolean"}
      }}},
    "authorizations":{"type":"array","maxItems":10000,"items":{"type":"object","additionalProperties":false,
      "required":["authorization_id","token_revision","has_access_token","has_refresh_token","access_token_expires_at","refresh_token_expires_at","pending_account_sync","advertiser_ids"],
      "properties":{
        "authorization_id":{"type":"string","minLength":1,"maxLength":256},"token_revision":{"type":"integer","minimum":1},
        "has_access_token":{"type":"boolean"},"has_refresh_token":{"type":"boolean"},
        "access_token_expires_at":{"type":"string","maxLength":128},"refresh_token_expires_at":{"type":"string","maxLength":128},
        "pending_account_sync":{"type":"boolean"},
        "advertiser_ids":{"type":"array","maxItems":100000,"items":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"}}
      }}}
  }
}`

const qianchuanAuthorizationOutputSchema = objectOneOf +qianchuanAuthorizationSuccessSchema + `,` + errorOutputSchema + `]}`

const qianchuanProductsInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "product_ids":{"type":"array","maxItems":30,"uniqueItems":true,"items":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"}},
    "product_name":{"type":"string","minLength":1,"maxLength":100},
    "limit":{"type":"integer","minimum":1,"maximum":100,"default":50}
  }
}`

const qianchuanProductsSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","total_count","displayed_count","truncated","items"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"total_count":{"type":"integer","minimum":0},
    "displayed_count":{"type":"integer","minimum":0,"maximum":100},"truncated":{"type":"boolean"},
    "items":{"type":"array","maxItems":100,"items":{"type":"object","additionalProperties":false,
      "required":["product_id","name","category","channel_id","channel_type","sell_number","stock_number","audit_time"],
      "properties":{
        "product_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"name":{"type":"string","maxLength":500},
        "category":{"type":"string","maxLength":500},"channel_id":{"type":"string","maxLength":128},
        "channel_type":{"type":"string","maxLength":128},"sell_number":{"type":["integer","null"],"minimum":0},
        "stock_number":{"type":["integer","null"],"minimum":0},"audit_time":{"type":"string","maxLength":128}
      }}}
  }
}`

const qianchuanProductsOutputSchema = objectOneOf +qianchuanProductsSuccessSchema + `,` + errorOutputSchema + `]}`

const qianchuanPlansInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"},
    "status":{"type":"string","enum":["ALL","ALL_INCLUDE_DELETED","DELIVERY_OK","DISABLE","AUDIT","DELETED","SYSTEM_DISABLE","OFFLINE_AUDIT","OFFLINE_BALANCE","OFFLINE_BUDGET","TIME_DONE","TIME_NO_REACH"],"default":"ALL"},
    "limit":{"type":"integer","minimum":1,"maximum":100,"default":50}
  }
}`

const qianchuanPlanItemSchema = `{
  "type":"object","additionalProperties":false,
  "required":["ad_id","name","status","opt_status","create_time","marketing_goal","creator_ids","budget","smart_bid_type","roi2_goal"],
  "properties":{
    "ad_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"name":{"type":"string","maxLength":500},
    "status":{"type":"string","maxLength":128},"opt_status":{"type":"string","maxLength":128},"create_time":{"type":"string","maxLength":128},
    "marketing_goal":{"type":"string","maxLength":128},"creator_ids":{"type":"array","maxItems":1000,"items":{"type":"string","maxLength":128}},
    "budget":{"type":["number","null"]},"smart_bid_type":{"type":"string","maxLength":128},"roi2_goal":{"type":["number","null"]}
  }
}`

const qianchuanPlansSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","date_range","status","total_count","displayed_count","truncated","items"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "date_range":{"type":"object","additionalProperties":false,"required":["start_date","end_date"],"properties":{"start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"}}},
    "status":{"type":"string","maxLength":128},"total_count":{"type":"integer","minimum":0},
    "displayed_count":{"type":"integer","minimum":0,"maximum":100},"truncated":{"type":"boolean"},
    "items":{"type":"array","maxItems":100,"items":` + qianchuanPlanItemSchema + `}
  }
}`

const qianchuanPlansOutputSchema = objectOneOf +qianchuanPlansSuccessSchema + `,` + errorOutputSchema + `]}`

const qianchuanPlanInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id","ad_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "ad_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"include_materials":{"type":"boolean","default":false}
  }
}`

const qianchuanPlanSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","plan","materials","material_count"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "plan":{"type":"object","additionalProperties":false,
      "required":["ad_id","name","status","opt_status","create_time","modify_time","marketing_goal","aweme_id","creators","products","budget","budget_mode","smart_bid_type","roi2_goal"],
      "properties":{
        "ad_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"name":{"type":"string","maxLength":500},"status":{"type":"string","maxLength":128},
        "opt_status":{"type":"string","maxLength":128},"create_time":{"type":"string","maxLength":128},"modify_time":{"type":"string","maxLength":128},
        "marketing_goal":{"type":"string","maxLength":128},"aweme_id":{"type":"string","maxLength":128},
        "creators":{"type":"array","maxItems":1000,"items":{"type":"object","additionalProperties":false,"required":["aweme_id","visible_id","name"],"properties":{
          "aweme_id":{"type":"string","maxLength":128},"visible_id":{"type":"string","maxLength":128},"name":{"type":"string","maxLength":500}
        }}},
        "products":{"type":"array","maxItems":30,"items":{"type":"object","additionalProperties":false,"required":["product_id","product_name","channel_id","channel_type"],"properties":{
          "product_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"product_name":{"type":"string","maxLength":500},
          "channel_id":{"type":"string","maxLength":128},"channel_type":{"type":"string","maxLength":128}
        }}},
        "budget":{"type":["number","null"]},"budget_mode":{"type":"string","maxLength":128},
        "smart_bid_type":{"type":"string","maxLength":128},"roi2_goal":{"type":["number","null"]}
      }},
    "materials":{"type":"array","maxItems":10000,"items":{"type":"object","additionalProperties":false,
      "required":["material_id","aweme_item_id","video_id","title","material_type","material_select_type","material_status","audit_status","duration","deleted","aweme_ids","product_ids"],
      "properties":{
        "material_id":{"type":"string","maxLength":128},"aweme_item_id":{"type":"string","maxLength":128},"video_id":{"type":"string","maxLength":128},
        "title":{"type":"string","maxLength":1000},"material_type":{"type":"string","maxLength":128},"material_select_type":{"type":"string","maxLength":128},
        "material_status":{"type":"string","maxLength":128},"audit_status":{"type":"string","maxLength":128},"duration":{"type":["integer","null"]},
        "deleted":{"type":["boolean","null"]},"aweme_ids":{"type":"array","maxItems":1000,"items":{"type":"string","maxLength":128}},
        "product_ids":{"type":"array","maxItems":1000,"items":{"type":"string","maxLength":128}}
      }}},
    "material_count":{"type":"integer","minimum":0}
  }
}`

const qianchuanPlanOutputSchema = objectOneOf +qianchuanPlanSuccessSchema + `,` + errorOutputSchema + `]}`

const qianchuanAccountReportInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"},
    "scope":{"type":"string","enum":["overall","uni"],"default":"overall"}
  }
}`

const reportMetricSchema = `{"type":["number","string","null"],"maxLength":128}`

const qianchuanAccountReportSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","scope","date_range","metrics"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"scope":{"type":"string","enum":["overall","uni"]},
    "date_range":{"type":"object","additionalProperties":false,"required":["start_date","end_date"],"properties":{"start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"}}},
    "metrics":{"type":"object","additionalProperties":false,
      "required":["stat_cost","stat_cost_for_roi2","stat_cost_for_overall_roi2","total_pay_order_count_for_roi2","total_pay_order_gmv_include_coupon_for_roi2","total_prepay_and_pay_order_roi2","total_order_settle_amount_for_roi2_1h","total_prepay_and_pay_settle_roi2_1h","total_prepay_and_pay_settle_overall_roi2_1h"],
      "properties":{
        "stat_cost":` + reportMetricSchema + `,"stat_cost_for_roi2":` + reportMetricSchema + `,"stat_cost_for_overall_roi2":` + reportMetricSchema + `,
        "total_pay_order_count_for_roi2":` + reportMetricSchema + `,"total_pay_order_gmv_include_coupon_for_roi2":` + reportMetricSchema + `,
        "total_prepay_and_pay_order_roi2":` + reportMetricSchema + `,"total_order_settle_amount_for_roi2_1h":` + reportMetricSchema + `,
        "total_prepay_and_pay_settle_roi2_1h":` + reportMetricSchema + `,"total_prepay_and_pay_settle_overall_roi2_1h":` + reportMetricSchema + `
      }}
  }
}`

const qianchuanAccountReportOutputSchema = objectOneOf +qianchuanAccountReportSuccessSchema + `,` + errorOutputSchema + `]}`

const qianchuanUniAccountReportInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"}
  }
}`

const qianchuanUniAccountReportSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","date_range","data"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "date_range":{"type":"object","additionalProperties":false,"required":["start_date","end_date"],"properties":{"start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"}}},
    "data":{"type":"object"},"_meta":{"type":"object"}
  }
}`

const qianchuanUniAccountReportOutputSchema = objectOneOf +qianchuanUniAccountReportSuccessSchema + `,` + errorOutputSchema + `]}`


const qianchuanPlanReportInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"},
    "status":{"type":"string","enum":["ALL","ALL_INCLUDE_DELETED","DELIVERY_OK","DISABLE","AUDIT","DELETED","SYSTEM_DISABLE","OFFLINE_AUDIT","OFFLINE_BALANCE","OFFLINE_BUDGET","TIME_DONE","TIME_NO_REACH"],"default":"ALL"},
    "limit":{"type":"integer","minimum":1,"maximum":100,"default":10}
  }
}`

const qianchuanPlanReportSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","date_range","amount_unit","summary","displayed_count","total_row_count","truncated","presentation","details"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "date_range":{"type":"object","additionalProperties":false,"required":["start_date","end_date"],"properties":{"start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"}}},
    "amount_unit":{"const":"CNY"},
    "summary":{"type":"object","additionalProperties":false,
      "required":["plan_count","plans_with_cost","metadata_missing_count","total_cost","total_pay_order_count","total_pay_order_gmv","total_pay_roi","total_settled_amount_1h","total_settled_roi_1h"],
      "properties":{
        "plan_count":{"type":"integer","minimum":0},"plans_with_cost":{"type":"integer","minimum":0},"metadata_missing_count":{"type":"integer","minimum":0},
        "total_cost":{"type":"number"},"total_pay_order_count":{"type":"integer","minimum":0},"total_pay_order_gmv":{"type":"number"},
        "total_pay_roi":{"type":"number"},"total_settled_amount_1h":{"type":"number"},"total_settled_roi_1h":{"type":"number"}
      }},
    "displayed_count":{"type":"integer","minimum":0,"maximum":100},"total_row_count":{"type":"integer","minimum":0},"truncated":{"type":"boolean"},
    "presentation":{"type":"object","additionalProperties":false,"required":["required","rendered_markdown"],"properties":{
      "required":{"const":true},"rendered_markdown":{"type":"string","minLength":1,"maxLength":2000000}
    }},
    "details":{"type":"array","maxItems":100,"items":{"type":"object","additionalProperties":false,
      "required":["ad_id","name","budget_mode_label","cost_guarantee_result","cost_guarantee_reason"],"properties":{
        "ad_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"name":{"type":"string","maxLength":500},
        "budget_mode_label":{"type":"string","maxLength":128},"cost_guarantee_result":{"type":"string","maxLength":128},
        "cost_guarantee_reason":{"type":"string","maxLength":1000}
      }}}
  }
}`

const qianchuanPlanReportOutputSchema = objectOneOf +qianchuanPlanReportSuccessSchema + `,` + errorOutputSchema + `]}`
