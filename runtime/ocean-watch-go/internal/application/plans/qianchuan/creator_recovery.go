package qianchuan

import (
	"context"
	"strings"

	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

// Recovery searches only the exact, explicitly supplied visible Douyin ID.
// Nicknames and numeric UIDs are never used as authorization search keywords.
func (verifier WorkVerifier) recoverVisibleCreatorHints(ctx context.Context, request WorkVerificationRequest, works []WorkInput, maxPages int, result *WorkVerificationResult) []WorkInput {
	type recovered struct{ uid, reason, message string }
	queries := map[string]recovered{}
	output := make([]WorkInput, 0, len(works))
	for _, work := range works {
		if work.OwnerHint == nil || work.OwnerHint.AwemeID != "" || strings.TrimSpace(work.OwnerHint.AwemeShowID) == "" {
			output = append(output, work)
			continue
		}
		showID := strings.TrimSpace(work.OwnerHint.AwemeShowID)
		match, queried := queries[showID]
		if !queried {
			matches := map[string]struct{}{}
			complete := false
			for page := 1; page <= maxPages; page++ {
				result.OwnerHintSummary.AuthorizedHintQueryCount++
				response, err := runRead(ctx, request.ReadPool, func(ctx context.Context) (domainqianchuan.AuthorizedCreatorPage, error) {
					return verifier.Reader.FetchAuthorizedCreators(ctx, portqianchuan.AuthorizedCreatorPageRequest{
						AdvertiserID: request.AdvertiserID, AccessToken: request.AccessToken,
						SearchKeyword: showID, MarketingGoal: "VIDEO_PROM_GOODS", Scene: "CREATE", Page: page, PageSize: 100,
					})
				})
				if err != nil || response.PageInfo.Page != page || response.PageInfo.TotalPages < 0 || response.PageInfo.TotalNumber < 0 {
					result.OwnerHintSummary.AuthorizedHintFailureCount++
					match.reason, match.message = "creator_query_incomplete", "达人身份定向查询未完成，授权状态未知"
					break
				}
				for _, creator := range response.Rows {
					if strings.TrimSpace(creator.VisibleID) == showID && validPositiveID(creator.AwemeID) {
						matches[creator.AwemeID] = struct{}{}
					}
				}
				if response.PageInfo.TotalPages == 0 || page >= response.PageInfo.TotalPages {
					complete = true
					break
				}
			}
			if match.reason == "" {
				switch {
				case !complete:
					match.reason, match.message = "creator_query_incomplete", "达人身份定向查询未完成，授权状态未知"
				case len(matches) == 0:
					match.reason, match.message = "creator_identity_not_found", "未找到与所提供抖音号完全匹配的达人身份"
				case len(matches) > 1:
					match.reason, match.message = "creator_identity_ambiguous", "所提供抖音号对应多个达人身份，需核实"
				default:
					for uid := range matches {
						match.uid = uid
					}
				}
			}
			queries[showID] = match
		}
		if match.reason != "" {
			result.Skipped = append(result.Skipped, skippedFromWork(work, match.reason, match.message, nil))
			continue
		}
		hint := *work.OwnerHint
		hint.AwemeID, hint.AwemeShowID = match.uid, showID
		work.OwnerHint = &hint
		output = append(output, work)
	}
	return output
}
