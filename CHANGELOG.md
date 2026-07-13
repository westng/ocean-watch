# Changelog

All notable changes to Ocean Watch are documented here.

## 0.7.0 - 2026-07-13

### Added

- Creator-authorized video discovery through the official Aweme authorization relationship API.
- Native promotion payloads using authorized `aweme_id`, `item_id`, `video_id`, and cover IDs.
- Source-bound schema v3 plan templates for account-uploaded and creator-authorized materials.
- Creator material authorization, expiry, advertiser ownership, and same-creator validation.
- Dedicated read-only query and dry-run-first creator creation commands.

### Changed

- New business template names include an explicit material-source suffix.
- Specific video, cover, item, and material IDs are runtime selections instead of schema v3 template fields.
- Legacy fixed material IDs require explicit confirmation before template migration removes them.

### Security

- Creator material development and regression tests use synthetic fixtures and never access local credentials or real advertiser accounts.
