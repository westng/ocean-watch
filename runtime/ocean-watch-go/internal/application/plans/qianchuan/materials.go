package qianchuan

import (
	"context"
	"errors"
	"fmt"

	domainqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain/qianchuan"
	portqianchuan "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/qianchuan"
)

func fetchAllPlanMaterials(
	ctx context.Context,
	reader portqianchuan.Reader,
	advertiserID string,
	accessToken string,
	adID string,
) ([]domainqianchuan.PlanMaterial, error) {
	if ctx == nil {
		return nil, errors.New("Qianchuan plan material context is required")
	}
	if reader == nil {
		return nil, errors.New("Qianchuan plan material reader is required")
	}
	rows := []domainqianchuan.PlanMaterial{}
	expectedPages, expectedTotal := -1, -1
	for page := 1; page <= CurrentPlanMaxPages; page++ {
		result, err := reader.FetchPlanMaterials(ctx, portqianchuan.MaterialPageRequest{
			AdvertiserID: advertiserID, AccessToken: accessToken, AdID: adID,
			MaterialType: "VIDEO", MaterialStatus: "ALL", Page: page, PageSize: 100,
		})
		if err != nil {
			return nil, fmt.Errorf("query Qianchuan plan materials page %d: %w", page, err)
		}
		info := result.PageInfo
		if info.Page != page || info.TotalPages < 0 || info.TotalNumber < 0 {
			return nil, fmt.Errorf("Qianchuan plan materials page %d returned invalid pagination metadata", page)
		}
		if info.TotalPages == 0 {
			if page != 1 || info.TotalNumber != 0 || len(result.Rows) != 0 {
				return nil, errors.New("Qianchuan plan materials returned contradictory empty pagination metadata")
			}
			return rows, nil
		}
		if info.TotalPages > CurrentPlanMaxPages || page > info.TotalPages {
			return nil, errors.New("Qianchuan plan material pagination exceeds its safety limit")
		}
		if expectedPages < 0 {
			expectedPages, expectedTotal = info.TotalPages, info.TotalNumber
		} else if info.TotalPages != expectedPages || info.TotalNumber != expectedTotal {
			return nil, fmt.Errorf("Qianchuan plan materials page %d changed pagination totals", page)
		}
		rows = append(rows, result.Rows...)
		if page == expectedPages {
			if len(rows) != expectedTotal {
				return nil, fmt.Errorf(
					"Qianchuan plan materials returned %d rows but declared %d",
					len(rows), expectedTotal,
				)
			}
			return rows, nil
		}
	}
	return nil, errors.New("Qianchuan plan material pagination exceeds its safety limit")
}
