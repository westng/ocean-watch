package workmetadata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

const (
	DefaultTimeout = 15 * time.Second
	MaxRedirects   = 5
)

var (
	workURLPattern  = regexp.MustCompile(`(?i)https?://[^\s]+`)
	workPathPattern = regexp.MustCompile(`(?:^|/)video/([0-9]+)(?:/|$)`)
)

type DouyinRedirectResolver struct {
	Client *http.Client
}

func (resolver DouyinRedirectResolver) Resolve(
	ctx context.Context,
	value string,
) (domain.ResolvedWorkLink, error) {
	inputURL, err := NormalizeDouyinURL(value)
	if err != nil {
		return domain.ResolvedWorkLink{}, err
	}
	if itemID := WorkIDFromURL(inputURL); itemID != "" {
		return resolvedWorkLink(inputURL, inputURL, itemID), nil
	}
	client := resolver.Client
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	} else {
		copy := *client
		client = &copy
		if client.Timeout <= 0 || client.Timeout > DefaultTimeout {
			client.Timeout = DefaultTimeout
		}
	}
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) > MaxRedirects {
			return domain.NewWorkLinkError("too_many_redirects", "作品链接跳转次数超过安全限制")
		}
		if _, validateErr := ValidateDouyinURL(request.URL); validateErr != nil {
			return validateErr
		}
		if previousRedirect != nil {
			return previousRedirect(request, via)
		}
		return nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, inputURL, nil)
	if err != nil {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("invalid_url", "作品链接必须是有效的 HTTPS 地址")
	}
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Range", "bytes=0-0")
	request.Header.Set("User-Agent", "Mozilla/5.0 AppleWebKit/537.36 Chrome/126 Safari/537.36")
	response, err := client.Do(request)
	if err != nil {
		var linkErr *domain.WorkLinkError
		if errors.As(err, &linkErr) {
			return domain.ResolvedWorkLink{}, linkErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return domain.ResolvedWorkLink{}, err
		}
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("redirect_failed", "作品短链解析失败")
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.CopyN(io.Discard, response.Body, 1)
	resolvedURL, err := ValidateDouyinURL(response.Request.URL)
	if err != nil {
		return domain.ResolvedWorkLink{}, err
	}
	itemID := WorkIDFromURL(resolvedURL)
	if itemID == "" {
		return domain.ResolvedWorkLink{}, domain.NewWorkLinkError("missing_work_id", "作品链接跳转后未包含 /video/{作品ID}")
	}
	return resolvedWorkLink(inputURL, resolvedURL, itemID), nil
}

func NormalizeDouyinURL(value string) (string, error) {
	text := strings.TrimSpace(value)
	if match := workURLPattern.FindString(text); match != "" {
		text = strings.TrimRight(match, ".,;:)]}>，。；：）】》")
	}
	parsed, err := url.Parse(text)
	if err != nil {
		return "", domain.NewWorkLinkError("invalid_url", "作品链接必须是有效的 HTTPS 地址")
	}
	return ValidateDouyinURL(parsed)
}

func ValidateDouyinURL(parsed *url.URL) (string, error) {
	if parsed == nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return "", domain.NewWorkLinkError("invalid_url", "作品链接必须是无凭据、无片段的 HTTPS 地址")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if !isDouyinHost(host) {
		return "", domain.NewWorkLinkError("untrusted_host", "作品链接不属于受信任的抖音域名")
	}
	port := parsed.Port()
	if port != "" && port != "443" {
		return "", domain.NewWorkLinkError("untrusted_port", "作品链接使用了不允许的端口")
	}
	if strings.Contains(parsed.Host, ":") && port == "" {
		return "", domain.NewWorkLinkError("invalid_url", "作品链接端口无效")
	}
	parsed.Scheme = "https"
	parsed.Host = host
	if port == "443" {
		parsed.Host = host + ":443"
	}
	return parsed.String(), nil
}

func WorkIDFromURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	match := workPathPattern.FindStringSubmatch(parsed.EscapedPath())
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func CanonicalWorkURL(itemID string) string {
	return fmt.Sprintf("https://www.douyin.com/video/%s", itemID)
}

func isDouyinHost(host string) bool {
	for _, suffix := range []string{"douyin.com", "iesdouyin.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func resolvedWorkLink(inputURL, resolvedURL, itemID string) domain.ResolvedWorkLink {
	return domain.ResolvedWorkLink{
		InputURL: inputURL, ResolvedURL: resolvedURL,
		CanonicalURL: CanonicalWorkURL(itemID), AwemeItemID: itemID,
	}
}
