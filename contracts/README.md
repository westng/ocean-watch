# Stable Output Contracts

This directory defines the public output shape of the Ocean Watch Go CLI.

- `output/cli-envelope.schema.json` defines the shared success and error envelope.
- `output/presentation.schema.json` defines mandatory table and Markdown presentation fields.
- `presentation/` contains stable Markdown examples for account lists, account reports, and Qianchuan batch results.

Treat these files as user-facing compatibility contracts. Review schema, column,
ordering, label, and Markdown changes together with the Go command that produces
them. Never store credentials, advertiser data, work links, or raw official
responses here.
