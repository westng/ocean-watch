package mcpserver

const marketingAuthorizationInputSchema = qianchuanAuthorizationInputSchema
const marketingAuthorizationOutputSchema = qianchuanAuthorizationOutputSchema

const marketingVideosInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "video_ids":{"type":"array","maxItems":100,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":128}},
    "material_ids":{"type":"array","maxItems":100,"uniqueItems":true,"items":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"}},
    "signatures":{"type":"array","maxItems":100,"uniqueItems":true,"items":{"type":"string","minLength":1,"maxLength":512}},
    "filename":{"type":"string","maxLength":500},"start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"},
    "page":{"type":"integer","minimum":1,"maximum":10000,"default":1},"limit":{"type":"integer","minimum":1,"maximum":100,"default":50}
  }
}`

const marketingVideoItemSchema = `{
  "type":"object","additionalProperties":false,
  "required":["video_id","material_id","filename","create_time","width","height","duration","format","source","signature"],
  "properties":{
    "video_id":{"type":"string","maxLength":128},"material_id":{"type":"string","maxLength":128},"filename":{"type":"string","maxLength":1000},
    "create_time":{"type":"string","maxLength":128},"width":{"type":["integer","null"]},"height":{"type":["integer","null"]},
    "duration":{"type":["number","null"]},"format":{"type":"string","maxLength":128},"source":{"type":"string","maxLength":128},
    "signature":{"type":"string","maxLength":512}
  }
}`

const marketingVideosSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","page","total_count","displayed_count","has_more","items"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"page":{"type":"integer","minimum":1,"maximum":10000},
    "total_count":{"type":"integer","minimum":0},"displayed_count":{"type":"integer","minimum":0,"maximum":100},"has_more":{"type":"boolean"},
    "items":{"type":"array","maxItems":100,"items":` + marketingVideoItemSchema + `}
  }
}`

const marketingVideosOutputSchema = `{"oneOf":[` + marketingVideosSuccessSchema + `,` + errorOutputSchema + `]}`

const marketingCreatorInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "source":{"type":"string","enum":["authorized","homepage"],"default":"authorized"},
    "aweme_ids":{"type":"array","maxItems":100,"uniqueItems":true,"items":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"}},
    "item_ids":{"type":"array","maxItems":100,"uniqueItems":true,"items":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"}},
    "minimum_remaining_days":{"type":"integer","minimum":0,"maximum":3650,"default":1},"include_unusable":{"type":"boolean","default":false},
    "page":{"type":"integer","minimum":1,"maximum":10000,"default":1},
    "limit":{"type":"integer","minimum":1,"maximum":100,"default":50}
  }
}`

const marketingCreatorSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","material_source","page","source_total_count","displayed_count","has_more","items"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"material_source":{"type":"string","maxLength":128},
    "page":{"type":"integer","minimum":1,"maximum":10000},"source_total_count":{"type":"integer","minimum":0},
    "displayed_count":{"type":"integer","minimum":0,"maximum":100},"has_more":{"type":"boolean"},
    "items":{"type":"array","maxItems":100,"items":{"type":"object","additionalProperties":false,
      "required":["material_id","video_id","item_id","image_mode","video_cover_id","title","duration","creator_id","creator_name","authorization_subject_id","authorization_type","authorization_status","authorization_start_at","authorization_expires_at","warning_types","usable","unusable_reasons"],
      "properties":{
        "material_id":{"type":"string","maxLength":128},"video_id":{"type":"string","maxLength":128},"item_id":{"type":"string","maxLength":128},
        "image_mode":{"type":"string","maxLength":128},"video_cover_id":{"type":"string","maxLength":128},"title":{"type":"string","maxLength":2000},
        "duration":{"type":["number","null"]},"creator_id":{"type":"string","maxLength":128},"creator_name":{"type":"string","maxLength":500},
        "authorization_subject_id":{"type":"string","maxLength":128},"authorization_type":{"type":"string","maxLength":128},
        "authorization_status":{"type":"string","maxLength":128},"authorization_start_at":{"type":"string","maxLength":128},
        "authorization_expires_at":{"type":"string","maxLength":128},"warning_types":{"type":"array","maxItems":100,"items":{"type":"string","maxLength":256}},
        "usable":{"type":"boolean"},"unusable_reasons":{"type":"array","maxItems":100,"items":{"type":"string","maxLength":256}}
      }
    }}
  }
}`

const marketingCreatorOutputSchema = `{"oneOf":[` + marketingCreatorSuccessSchema + `,` + errorOutputSchema + `]}`

const marketingPlanReportInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"},
    "limit":{"type":"integer","minimum":1,"maximum":100,"default":10}
  }
}`

const marketingNullableMetricSchema = `{"type":["string","null"],"maxLength":128}`

const marketingPlanReportSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","date_range","amount_unit","summary","total_row_count","displayed_count","truncated","presentation","items"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "date_range":{"type":"object","additionalProperties":false,"required":["start_date","end_date"],"properties":{"start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"}}},
    "amount_unit":{"const":"CNY"},
    "summary":{"type":"object","additionalProperties":false,"required":["total_spend","total_gmv","total_orders","weighted_roi","plans_with_spend"],"properties":{
      "total_spend":{"type":"number"},"total_gmv":{"type":["number","null"]},"total_orders":{"type":["integer","null"]},
      "weighted_roi":{"type":["number","null"]},"plans_with_spend":{"type":"integer","minimum":0}
    }},
    "total_row_count":{"type":"integer","minimum":0},"displayed_count":{"type":"integer","minimum":0,"maximum":100},"truncated":{"type":"boolean"},
    "presentation":{"type":"object","additionalProperties":false,"required":["required","rendered_markdown"],"properties":{"required":{"const":true},"rendered_markdown":{"type":"string","minLength":1,"maxLength":2000000}}},
    "items":{"type":"array","maxItems":100,"items":{"type":"object","additionalProperties":false,
      "required":["project_id","project_name","stat_cost","show_count","click_count","ctr","convert_count","conversion_cost","conversion_rate","in_app_order_count","in_app_order_gmv","in_app_order_roi"],
      "properties":{
        "project_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"project_name":{"type":"string","maxLength":500},
        "stat_cost":` + marketingNullableMetricSchema + `,"show_count":` + marketingNullableMetricSchema + `,"click_count":` + marketingNullableMetricSchema + `,
        "ctr":` + marketingNullableMetricSchema + `,"convert_count":` + marketingNullableMetricSchema + `,"conversion_cost":` + marketingNullableMetricSchema + `,
        "conversion_rate":` + marketingNullableMetricSchema + `,"in_app_order_count":` + marketingNullableMetricSchema + `,
        "in_app_order_gmv":` + marketingNullableMetricSchema + `,"in_app_order_roi":` + marketingNullableMetricSchema + `
      }
    }}
  }
}`

const marketingPlanReportOutputSchema = `{"oneOf":[` + marketingPlanReportSuccessSchema + `,` + errorOutputSchema + `]}`

const marketingMaterialReportInputSchema = `{
  "type":"object","additionalProperties":false,"required":["advertiser_id"],
  "properties":{
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"auth_account_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"},"project_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "promotion_ids":{"type":"array","maxItems":100,"uniqueItems":true,"items":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"}},
    "active_only":{"type":"boolean","default":false},"limit":{"type":"integer","minimum":1,"maximum":100,"default":50}
  }
}`

const marketingMaterialReportSuccessSchema = `{
  "$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,
  "required":["ok","request_id","source","advertiser_id","date_range","amount_unit","summary","total_row_count","displayed_count","truncated","items"],
  "properties":{
    "ok":{"const":true},"request_id":{"type":"string","minLength":1,"maxLength":128},"source":{"const":"official_api"},
    "advertiser_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
    "date_range":{"type":"object","additionalProperties":false,"required":["start_date","end_date"],"properties":{"start_date":{"type":"string","format":"date"},"end_date":{"type":"string","format":"date"}}},
    "amount_unit":{"const":"CNY"},
    "summary":{"type":"object","additionalProperties":false,"required":["promotion_count","selected_promotion_count","active_promotion_count","excluded_promotion_count","material_count","rows_with_report_data","rows_without_report_data"],"properties":{
      "promotion_count":{"type":"integer","minimum":0},"selected_promotion_count":{"type":"integer","minimum":0},"active_promotion_count":{"type":"integer","minimum":0},
      "excluded_promotion_count":{"type":"integer","minimum":0},"material_count":{"type":"integer","minimum":0},"rows_with_report_data":{"type":"integer","minimum":0},
      "rows_without_report_data":{"type":"integer","minimum":0}
    }},
    "total_row_count":{"type":"integer","minimum":0},"displayed_count":{"type":"integer","minimum":0,"maximum":100},"truncated":{"type":"boolean"},
    "items":{"type":"array","maxItems":100,"items":{"type":"object","additionalProperties":false,
      "required":["project_id","promotion_id","promotion_name","promotion_status","promotion_opt_status","material_id","video_id","video_cover_id","material_status","material_opt_status","image_mode","material_create_time","has_report_data","stat_cost","show_count","click_count","ctr","cpc","cpm","convert_count","conversion_cost","conversion_rate","total_play","play_duration_3s","play_over_rate","in_app_order","in_app_order_gmv","in_app_order_roi"],
      "properties":{
        "project_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"promotion_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},
        "promotion_name":{"type":"string","maxLength":1000},"promotion_status":{"type":"string","maxLength":1000},"promotion_opt_status":{"type":"string","maxLength":1000},
        "material_id":{"type":"string","pattern":"^[1-9][0-9]{0,18}$"},"video_id":{"type":"string","maxLength":1000},"video_cover_id":{"type":"string","maxLength":1000},
        "material_status":{"type":"string","maxLength":1000},"material_opt_status":{"type":"string","maxLength":1000},"image_mode":{"type":"string","maxLength":1000},
        "material_create_time":{"type":"string","maxLength":1000},"has_report_data":{"type":"boolean"},
        "stat_cost":` + marketingNullableMetricSchema + `,"show_count":` + marketingNullableMetricSchema + `,"click_count":` + marketingNullableMetricSchema + `,
        "ctr":` + marketingNullableMetricSchema + `,"cpc":` + marketingNullableMetricSchema + `,"cpm":` + marketingNullableMetricSchema + `,
        "convert_count":` + marketingNullableMetricSchema + `,"conversion_cost":` + marketingNullableMetricSchema + `,"conversion_rate":` + marketingNullableMetricSchema + `,
        "total_play":` + marketingNullableMetricSchema + `,"play_duration_3s":` + marketingNullableMetricSchema + `,"play_over_rate":` + marketingNullableMetricSchema + `,
        "in_app_order":` + marketingNullableMetricSchema + `,"in_app_order_gmv":` + marketingNullableMetricSchema + `,"in_app_order_roi":` + marketingNullableMetricSchema + `
      }
    }}
  }
}`

const marketingMaterialReportOutputSchema = `{"oneOf":[` + marketingMaterialReportSuccessSchema + `,` + errorOutputSchema + `]}`
