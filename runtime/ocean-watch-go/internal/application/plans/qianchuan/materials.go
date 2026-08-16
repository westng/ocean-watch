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
	return fetchAllPlanMaterialsWithPool(ctx, reader, advertiserID, accessToken, adID, nil)
}

func fetchAllPlanMaterialsWithPool(
	ctx context.Context,
	reader portqianchuan.Reader,
	advertiserID string,
	accessToken string,
	adID string,
	pool *ReadPool,
) ([]domainqianchuan.PlanMaterial, error) {
	if ctx == nil {
		return nil, errors.New("Qianchuan plan material context is required")
	}
	if reader == nil {
		return nil, errors.New("Qianchuan plan material reader is required")
	}
	fetch := func(ctx context.Context, page int) (domainqianchuan.MaterialPage, error) {
		return runRead(ctx, pool, func(ctx context.Context) (domainqianchuan.MaterialPage, error) {
			return reader.FetchPlanMaterials(ctx, portqianchuan.MaterialPageRequest{
				AdvertiserID: advertiserID, AccessToken: accessToken, AdID: adID,
				MaterialType: "VIDEO", MaterialStatus: "ALL", Page: page, PageSize: 100,
			})
		})
	}
	first, err := fetch(ctx, 1)
	if err != nil {
		return nil, fmt.Errorf("query Qianchuan plan materials page 1: %w", err)
	}
	if first.PageInfo.Page != 1 || first.PageInfo.TotalPages < 0 || first.PageInfo.TotalNumber < 0 {
		return nil, errors.New("Qianchuan plan materials page 1 returned invalid pagination metadata")
	}
	if first.PageInfo.TotalPages == 0 {
		if first.PageInfo.TotalNumber != 0 || len(first.Rows) != 0 {
			return nil, errors.New("Qianchuan plan materials returned contradictory empty pagination metadata")
		}
		return []domainqianchuan.PlanMaterial{}, nil
	}
	if first.PageInfo.TotalPages > CurrentPlanMaxPages {
		return nil, errors.New("Qianchuan plan material pagination exceeds its safety limit")
	}
	type pageResult struct {
		page domainqianchuan.MaterialPage
		err  error
	}
	remaining := parallelOrdered(ctx, pool, first.PageInfo.TotalPages-1, func(ctx context.Context, index int) pageResult {
		page := index + 2
		result, fetchErr := fetch(ctx, page)
		if fetchErr != nil {
			fetchErr = fmt.Errorf("query Qianchuan plan materials page %d: %w", page, fetchErr)
		}
		return pageResult{page: result, err: fetchErr}
	})
	rows := append([]domainqianchuan.PlanMaterial(nil), first.Rows...)
	for index, fetched := range remaining {
		page := index + 2
		if fetched.err != nil {
			return nil, fetched.err
		}
		info := fetched.page.PageInfo
		if info.Page != page || info.TotalPages != first.PageInfo.TotalPages ||
			info.TotalNumber != first.PageInfo.TotalNumber {
			return nil, fmt.Errorf("Qianchuan plan materials page %d changed pagination totals", page)
		}
		rows = append(rows, fetched.page.Rows...)
	}
	if len(rows) != first.PageInfo.TotalNumber {
		return nil, fmt.Errorf(
			"Qianchuan plan materials returned %d rows but declared %d",
			len(rows), first.PageInfo.TotalNumber,
		)
	}
	return rows, nil
}
