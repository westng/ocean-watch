# Creator-authorized video API notes

Official sources:

- Authorization relationships: `https://open.oceanengine.com/labels/7/docs/1729983667746823?origin=left_nav`
- Promotion create: `https://open.oceanengine.com/labels/34/docs/1740946299496459?origin=left_nav`

## Query

Use `GET /open_api/2/tools/aweme_auth_list/` with:

- `advertiser_id`
- `filtering.auth_type: ["VIDEO_ITEM"]`
- `filtering.auth_status: ["AUTHRIZED"]`
- optional `filtering.aweme_ids`
- optional `filtering.item_ids`
- `page` and `page_size`

The official status is spelled `AUTHRIZED`. Do not silently replace it with the English spelling.
The local candidate model exposes active authorization as `authorization_status: VALID` and
preserves the official value separately as `raw_authorization_status: AUTHRIZED`.

Relevant response fields:

- `auth_type`
- `auth_status`
- `aweme_id`
- `aweme_name`
- `start_time`
- `end_time`
- `warning_types`
- `video_info.item_id`
- `video_info.video_id`
- `video_info.video_cover_id`
- `video_info.mid`
- `video_info.image_mode`
- `video_info.title`

All official IDs are decoded losslessly and normalized to decimal strings in the local domain model. `item_id` is converted to a Python integer only at the final promotion payload boundary because the official create schema declares it as a number.

## Promotion payload

Creator-authorized videos use the normal `promotion_materials.video_material_list`, but each selected item contains:

- `video_id`
- `video_cover_id`
- `item_id`
- `image_mode`

The promotion also contains:

```json
{
  "native_setting": {
    "aweme_id": "AUTHORIZED_AWEME_ID"
  }
}
```

The ordinary native-unit path supports one `aweme_id`, so one unit must not mix materials from different creators. Multi-creator support would require a separate, officially qualified Star Joint Delivery flow and is outside the initial creator-material mode.

## Development boundary

The development tests use synthetic fixtures only. They do not read local OAuth credentials, project config, run history, or call Ocean Engine business APIs.
