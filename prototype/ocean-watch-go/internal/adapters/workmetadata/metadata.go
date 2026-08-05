package workmetadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/domain"
	portworkmetadata "github.com/westng/ocean-watch/prototype/ocean-watch-go/internal/ports/workmetadata"
)

const MaxMetadataResponseBytes = int64(1 << 20)

type DouyinMetadataResolver struct {
	Endpoint string
	Client   *http.Client
	Fallback portworkmetadata.Resolver
}

type metadataEnvelope struct {
	Code int          `json:"code"`
	Data metadataData `json:"data"`
}

type metadataData struct {
	Video   metadataVideo   `json:"video"`
	Author  metadataAuthor  `json:"author"`
	Product metadataProduct `json:"product"`
}

type metadataVideo struct {
	VideoInfoID any `json:"video_info_id"`
}

type metadataAuthor struct {
	UID      any    `json:"uid"`
	UniqueID string `json:"unique_id"`
	Nickname string `json:"nickname"`
}

type metadataProduct struct {
	ProductInfoID   any    `json:"product_info_id"`
	ProductInfoName string `json:"product_info_name"`
}

func (resolver DouyinMetadataResolver) Resolve(
	ctx context.Context,
	value string,
) (domain.ResolvedWorkLink, error) {
	inputURL, err := NormalizeDouyinURL(value)
	if err != nil {
		return domain.ResolvedWorkLink{}, err
	}
	metadata, err := resolver.resolveMetadata(ctx, inputURL)
	if err == nil {
		inputItemID := WorkIDFromURL(inputURL)
		if inputItemID == "" || inputItemID == metadata.AwemeItemID {
			metadata.InputURL = inputURL
			metadata.ResolvedURL = CanonicalWorkURL(metadata.AwemeItemID)
			metadata.CanonicalURL = metadata.ResolvedURL
			return metadata, nil
		}
		err = domain.NewWorkLinkError("metadata_work_mismatch", "作品解析服务返回的作品 ID 与输入链接不一致")
	}
	fallback := resolver.Fallback
	if fallback == nil {
		fallback = DouyinRedirectResolver{Client: resolver.Client}
	}
	result, fallbackErr := fallback.Resolve(ctx, inputURL)
	if fallbackErr != nil {
		return domain.ResolvedWorkLink{}, fallbackErr
	}
	result.HintWarning = metadataWarning(err)
	return result, nil
}

func (resolver DouyinMetadataResolver) resolveMetadata(
	ctx context.Context,
	inputURL string,
) (domain.ResolvedWorkLink, error) {
	endpoint, err := domain.ValidateWorkMetadataEndpoint(resolver.Endpoint)
	if err != nil || endpoint == "" {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("invalid_metadata_endpoint", "作品解析服务必须使用无凭据的 HTTPS 地址")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("invalid_metadata_endpoint", "作品解析服务必须使用无凭据的 HTTPS 地址")
	}
	query := parsed.Query()
	query.Add("url", inputURL)
	parsed.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("metadata_query_failed", "作品解析服务请求失败")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "ocean-watch/0.9")
	client := metadataHTTPClient(resolver.Client)
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return domain.ResolvedWorkLink{}, err
		}
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("metadata_query_failed", "作品解析服务请求失败")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("metadata_query_failed", "作品解析服务请求失败")
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, MaxMetadataResponseBytes+1))
	if err != nil {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("metadata_query_failed", "作品解析服务响应读取失败")
	}
	if int64(len(payload)) > MaxMetadataResponseBytes {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("metadata_response_too_large", "作品解析服务响应超过大小限制")
	}
	var envelope metadataEnvelope
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("invalid_metadata_response", "作品解析服务返回了无效 JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("invalid_metadata_response", "作品解析服务返回了无效 JSON")
	}
	itemID := metadataID(envelope.Data.Video.VideoInfoID)
	if envelope.Code != 200 || !positiveMetadataID(itemID) {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("invalid_metadata_response", "作品解析服务未返回有效作品 ID")
	}
	result := domain.ResolvedWorkLink{
		AwemeItemID: itemID, CreatorName: strings.TrimSpace(envelope.Data.Author.Nickname),
	}
	awemeID := metadataID(envelope.Data.Author.UID)
	showID := strings.TrimSpace(envelope.Data.Author.UniqueID)
	if positiveMetadataID(awemeID) {
		result.OwnerHint = &domain.WorkOwnerHint{AwemeID: awemeID, AwemeShowID: showID}
	}
	productID := metadataID(envelope.Data.Product.ProductInfoID)
	if positiveMetadataID(productID) {
		result.ProductHint = &domain.WorkProductHint{
			ProductID: productID, ProductName: strings.TrimSpace(envelope.Data.Product.ProductInfoName),
		}
	}
	return result, nil
}

func metadataHTTPClient(source *http.Client) *http.Client {
	client := &http.Client{Timeout: DefaultTimeout}
	if source != nil {
		copy := *source
		client = &copy
		if client.Timeout <= 0 || client.Timeout > DefaultTimeout {
			client.Timeout = DefaultTimeout
		}
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return client
}

func metadataID(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func positiveMetadataID(value string) bool {
	if value == "" || value == "0" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func metadataWarning(err error) *domain.WorkHintWarning {
	if err == nil {
		return nil
	}
	var linkErr *domain.WorkLinkError
	if errors.As(err, &linkErr) {
		return &domain.WorkHintWarning{Code: linkErr.Code, Message: linkErr.Message}
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &domain.WorkHintWarning{Code: "metadata_query_failed", Message: "作品解析服务请求失败"}
	}
	return &domain.WorkHintWarning{Code: "metadata_query_failed", Message: "作品解析服务请求失败"}
}
