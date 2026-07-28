package qianchuan

import "context"

const (
	OwnerHintCacheSchemaVersion = 1
	OwnerHintCacheTTLDays       = 30
	OwnerHintCacheMaxEntries    = 10000
)

type OwnerHint struct {
	AwemeID     string `json:"aweme_id"`
	AwemeShowID string `json:"aweme_show_id,omitempty"`
}

type OwnerHintCache interface {
	Load(context.Context, string, []string) (map[string]OwnerHint, error)
	Store(context.Context, string, map[string]OwnerHint) (int, error)
}

type OwnerHintSummary struct {
	Supplied                   int `json:"supplied"`
	Eligible                   int `json:"eligible"`
	Verified                   int `json:"verified"`
	Stale                      int `json:"stale"`
	BroadScanWorkCount         int `json:"broad_scan_work_count"`
	AuthorizedHintQueryCount   int `json:"authorized_hint_query_count"`
	AuthorizedHintFailureCount int `json:"authorized_hint_failure_count"`
	OfficialVideoQueryCount    int `json:"official_video_query_count"`
}
