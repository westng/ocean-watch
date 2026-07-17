# Creator-authorized video API notes

Official sources:

- Homepage videos: `https://open.oceanengine.com/labels/7/docs/1729982871844879?origin=left_nav`
- Authorization relationships: `https://open.oceanengine.com/labels/7/docs/1729983667746823?origin=left_nav`
- Promotion create: `https://open.oceanengine.com/labels/34/docs/1740946299496459?origin=left_nav`

## Query

Use `GET /open_api/2/file/video/aweme/get/` when the user supplies an `aweme_id`
and wants public videos from that creator's Douyin homepage. The `aweme_id` is a
top-level required string. This endpoint only returns public homepage videos and
requires the creator to be bound to the advertiser account.

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

Numeric official IDs such as `advertiser_id`, `item_id`, and `mid` are decoded
losslessly and normalized to decimal strings. `aweme_id`, `video_id`, and
`video_cover_id` are opaque strings and must not be restricted to decimal digits.
`item_id` is converted to a Python integer only at the final promotion payload
boundary because the official create schema declares it as a number.

Treat homepage videos and cooperation-authorized videos as separate query branches.
Do not infer cooperation authorization merely because a video is visible on a homepage.
Even when the official relationship endpoint ignores an `aweme_ids` or `item_ids`
filter, apply the same filters locally before returning candidates.

Authorization and API usability do not establish that a video matches the product in
the selected plan template. Before automatic selection, compare the candidate title
and available product context with `bindings.product_name`. Exclude clear conflicts
such as a fiber-drink video under a protein-powder template. Treat generic or
ambiguous titles as requiring user confirmation instead of silently selecting them.

The authorization relationship endpoint can temporarily omit `video_cover_id` even when an
existing official promotion still contains a valid cover for the same work. During plan preflight,
recover a missing cover only when `/v3.0/promotion/list/` yields one distinct cover under the same
advertiser, `item_id`, and `material_id`, with `material_status=MATERIAL_STATUS_OK`. Never persist
the recovered cover in a template, and block when no match or multiple cover IDs remain.
Historical cover recovery does not prove that the work is still inside its authorization period;
`/v3.0/promotion/create/` performs the final check. After that endpoint rejects a work as outside
the authorization period, keep the created project for promotion-only resume and block retries
until the refreshed relationship snapshot returns its own cover.

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

The checked official schema does not establish a numeric maximum for `video_material_list`. A creator template may therefore set `max_materials_per_unit` to `null`, meaning Ocean Watch applies no client-side count cap. This does not override any account-specific validation returned by the official create API.

## Development boundary

The development tests use synthetic fixtures only. They do not read local OAuth credentials, project config, run history, or call Ocean Engine business APIs.
