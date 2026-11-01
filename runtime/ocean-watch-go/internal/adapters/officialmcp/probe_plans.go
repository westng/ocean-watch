package officialmcp

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

var ErrProbePaging = errors.New("official MCP probe paging contract rejected")

const probePlanPageSize = 10

type ProbePlanPageParams struct {
	StartTime string
	EndTime   string
	Page      int32
}

type ProbePlanPageObservation struct {
	ProbeObservation
	Page      int32 `json:"page"`
	PageSize  int64 `json:"page_size"`
	Returned  int   `json:"returned"`
	HasMore   bool  `json:"has_more"`
	TotalPage int64 `json:"-"`
	TotalNum  int64 `json:"-"`
}

// Developer-only proof for one day of product, all-domain, custom-bid plans.
// The official API doc 1771195810853899 specifies default page size 10. The
// observed optional page_size schema has an integer/string-enum conflict:
// omit that field, retain the original schema validation, and verify the
// returned default. This is not a general paging or production task route.
func (probe *Probe) ReadPlanPage(ctx context.Context, advertiserID, approvedInputSourceDigest string, input ProbePlanPageParams) (ProbePlanPageObservation, error) {
	result := ProbePlanPageObservation{ProbeObservation: ProbeObservation{QueryID: probeListQuery}}
	id, err := strconv.ParseInt(advertiserID, 10, 64)
	if err != nil || id < 1 || strconv.FormatInt(id, 10) != advertiserID {
		return result, ErrProbeScope
	}
	start, startErr := time.Parse("2006-01-02 15:04:05", input.StartTime)
	end, endErr := time.Parse("2006-01-02 15:04:05", input.EndTime)
	if startErr != nil || endErr != nil || end.Before(start) || end.Sub(start) >= 24*time.Hour ||
		input.Page < 1 || input.Page > 2 {
		return result, ErrProbePaging
	}
	arguments := map[string]any{
		"advertiser_id": id, "start_time": input.StartTime, "end_time": input.EndTime,
		"marketing_goal": "VIDEO_PROM_GOODS", "adlab_scene": "UNI_PROJECT", "data_period": "ALL_DATA",
		"filtering": map[string]any{"status": "ALL", "having_cost": "ALL", "smart_bid_type": "SMART_BID_CUSTOM"},
		"fields":    []string{"stat_cost"}, "order_type": "DESC", "order_field": "create_time", "page": input.Page,
	}
	observation, err := probe.readQuery(ctx, probeListQuery, advertiserID, approvedInputSourceDigest, arguments)
	result.ProbeObservation = observation
	if err != nil {
		return result, err
	}
	data, ok := observation.Payload["data"].(map[string]any)
	if !ok {
		return rejectProbePage(result)
	}
	items, itemsOK := data["ad_list"].([]any)
	pageInfo, pageOK := data["page_info"].(map[string]any)
	page, pageErr := probePageInteger(pageInfo["page"])
	size, sizeErr := probePageInteger(pageInfo["page_size"])
	totalPage, totalPageErr := probePageInteger(pageInfo["total_page"])
	totalNum, totalNumErr := probePageInteger(pageInfo["total_num"])
	if !itemsOK || !pageOK || pageErr != nil || sizeErr != nil || totalPageErr != nil || totalNumErr != nil ||
		page != int64(input.Page) || size != probePlanPageSize || len(items) > probePlanPageSize {
		return rejectProbePage(result)
	}
	expectedPages := totalNum / probePlanPageSize
	if totalNum%probePlanPageSize != 0 {
		expectedPages++
	}
	if totalNum == 0 {
		if len(items) != 0 || input.Page != 1 || totalPage > 1 {
			return rejectProbePage(result)
		}
	} else {
		if totalPage != expectedPages || page > totalPage {
			return rejectProbePage(result)
		}
		expectedItems := int64(probePlanPageSize)
		if page == totalPage && totalNum%probePlanPageSize != 0 {
			expectedItems = totalNum % probePlanPageSize
		}
		if int64(len(items)) != expectedItems {
			return rejectProbePage(result)
		}
	}
	seen := map[string]bool{}
	for _, item := range items {
		row, rowOK := item.(map[string]any)
		info, infoOK := row["ad_info"].(map[string]any)
		planID, idErr := probePageInteger(info["id"])
		if !rowOK || !infoOK || idErr != nil || planID < 1 || seen[strconv.FormatInt(planID, 10)] {
			return rejectProbePage(result)
		}
		seen[strconv.FormatInt(planID, 10)] = true
		if info["marketing_goal"] != "VIDEO_PROM_GOODS" || info["adlab_scene"] != "UNI_PROJECT" ||
			info["smart_bid_type"] != "SMART_BID_CUSTOM" {
			return rejectProbePage(result)
		}
		stats, statsOK := row["stats_info"].(map[string]any)
		if _, numberOK := stats["stat_cost"].(json.Number); !statsOK || !numberOK {
			return rejectProbePage(result)
		}
	}
	result.Page, result.PageSize, result.Returned = input.Page, size, len(items)
	result.TotalPage, result.TotalNum, result.HasMore = totalPage, totalNum, page < totalPage
	return result, nil
}

func probePageInteger(value any) (int64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, ErrProbePaging
	}
	integer, err := strconv.ParseInt(number.String(), 10, 64)
	if err != nil || integer < 0 || strconv.FormatInt(integer, 10) != number.String() {
		return 0, ErrProbePaging
	}
	return integer, nil
}

func rejectProbePage(result ProbePlanPageObservation) (ProbePlanPageObservation, error) {
	result.BusinessSucceeded = false
	result.Payload = nil
	return result, ErrProbePaging
}
