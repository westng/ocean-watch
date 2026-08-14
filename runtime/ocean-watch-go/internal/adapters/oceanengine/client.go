package oceanengine

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	sdk "github.com/oceanengine/ad_open_sdk_go"
	sdkconfig "github.com/oceanengine/ad_open_sdk_go/config"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/platform/requestcontrol"
)

const (
	OfficialScheme        = "https"
	BusinessHost          = "api.oceanengine.com"
	OAuthHost             = "ad.oceanengine.com"
	DefaultRequestTimeout = 30 * time.Second
	DefaultMaxResponse    = int64(8 << 20)
	qianchuanEnvelopePeek = 64 << 10
)

type HostProfile string

const (
	ProfileBusiness       HostProfile = "official-business"
	ProfileOAuth          HostProfile = "official-oauth"
	ProfileAgent          HostProfile = "official-agent"
	ProfileQianchuanVideo HostProfile = "official-qianchuan-video"
)

type TimeoutProfile string

const TimeoutStandard TimeoutProfile = "standard"

type ClientProfile struct {
	Name             HostProfile
	Host             string
	Scheme           string
	TimeoutProfile   TimeoutProfile
	Timeout          time.Duration
	MaxResponseBytes int64
}

type FactoryOptions struct {
	TransportFactory       func(HostProfile) http.RoundTripper
	MaxResponseBytes       int64
	RequestGovernor        *requestcontrol.Governor
	RequestLimits          requestcontrol.Limits
	SharedQianchuanControl requestcontrol.SharedController
}

type ClientFactory struct {
	mu                     sync.Mutex
	clients                map[clientKey]*Client
	transportFactory       func(HostProfile) http.RoundTripper
	maxResponseBytes       int64
	requestGovernor        *requestcontrol.Governor
	sharedQianchuanControl requestcontrol.SharedController
}

type clientKey struct {
	channel        string
	hostProfile    HostProfile
	timeoutProfile TimeoutProfile
}

type Client struct {
	sdk     *sdk.Client
	profile ClientProfile
}

type SDKRequestError struct {
	cause      error
	dispatched bool
}

func (err *SDKRequestError) Error() string {
	return "Ocean Engine SDK request failed"
}

func (err *SDKRequestError) Unwrap() error {
	return err.cause
}

func (err *SDKRequestError) Dispatched() bool {
	return err != nil && err.dispatched
}

type notDispatchedError struct {
	cause error
}

func (err *notDispatchedError) Error() string {
	return "Ocean Engine SDK request was blocked before dispatch"
}

func (err *notDispatchedError) Unwrap() error {
	return err.cause
}

func NewClientFactory(options FactoryOptions) (*ClientFactory, error) {
	maxResponseBytes := options.MaxResponseBytes
	if maxResponseBytes <= 0 {
		maxResponseBytes = DefaultMaxResponse
	}
	if maxResponseBytes > DefaultMaxResponse {
		return nil, fmt.Errorf("Ocean Engine SDK response limit exceeds the approved %d bytes", DefaultMaxResponse)
	}
	transportFactory := options.TransportFactory
	if transportFactory == nil {
		transportFactory = func(HostProfile) http.RoundTripper { return newTransport() }
	}
	governor := options.RequestGovernor
	if governor == nil {
		var err error
		governor, err = requestcontrol.NewGovernor(options.RequestLimits)
		if err != nil {
			return nil, err
		}
	}
	return &ClientFactory{
		clients:                map[clientKey]*Client{},
		transportFactory:       transportFactory,
		maxResponseBytes:       maxResponseBytes,
		requestGovernor:        governor,
		sharedQianchuanControl: options.SharedQianchuanControl,
	}, nil
}

func (factory *ClientFactory) Client(
	channel string,
	hostProfile HostProfile,
	timeoutProfile TimeoutProfile,
) (*Client, error) {
	if channel != "marketing" && channel != "qianchuan" {
		return nil, fmt.Errorf("unsupported Ocean Engine channel %q", channel)
	}
	definition, err := resolveHostProfile(hostProfile)
	if err != nil {
		return nil, err
	}
	timeout, err := resolveTimeoutProfile(timeoutProfile)
	if err != nil {
		return nil, err
	}
	key := clientKey{channel: channel, hostProfile: hostProfile, timeoutProfile: timeoutProfile}
	factory.mu.Lock()
	defer factory.mu.Unlock()
	if existing := factory.clients[key]; existing != nil {
		return existing, nil
	}
	transport := factory.transportFactory(hostProfile)
	if transport == nil {
		return nil, errors.New("Ocean Engine SDK transport factory returned nil")
	}
	sharedQianchuanControl := factory.sharedQianchuanControl
	if channel == "qianchuan" && !qianchuanAdvertiserScopedProfile(hostProfile) {
		sharedQianchuanControl = nil
	}
	client := newClient(
		channel, definition, timeoutProfile, timeout, factory.maxResponseBytes,
		factory.requestGovernor, transport,
		sharedQianchuanControl,
	)
	factory.clients[key] = client
	return client, nil
}

func qianchuanAdvertiserScopedProfile(profile HostProfile) bool {
	return profile == ProfileBusiness || profile == ProfileQianchuanVideo
}

func resolveHostProfile(profile HostProfile) (ClientProfile, error) {
	switch profile {
	case ProfileBusiness:
		return ClientProfile{Name: profile, Host: BusinessHost, Scheme: OfficialScheme}, nil
	case ProfileOAuth, ProfileAgent, ProfileQianchuanVideo:
		return ClientProfile{Name: profile, Host: OAuthHost, Scheme: OfficialScheme}, nil
	default:
		return ClientProfile{}, fmt.Errorf("unsupported Ocean Engine host profile %q", profile)
	}
}

func resolveTimeoutProfile(profile TimeoutProfile) (time.Duration, error) {
	if profile == TimeoutStandard {
		return DefaultRequestTimeout, nil
	}
	return 0, fmt.Errorf("unsupported Ocean Engine timeout profile %q", profile)
}

func newClient(
	channel string,
	profile ClientProfile,
	timeoutProfile TimeoutProfile,
	timeout time.Duration,
	maxResponseBytes int64,
	governor *requestcontrol.Governor,
	transport http.RoundTripper,
	sharedQianchuanControl requestcontrol.SharedController,
) *Client {
	governedTransport := &governedTransport{
		base: transport, channel: channel, governor: governor,
		sharedQianchuanControl: sharedQianchuanControl,
	}
	guardedTransport := &securityTransport{
		base: governedTransport, host: profile.Host, scheme: profile.Scheme,
		maxResponseBytes: maxResponseBytes,
	}
	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: guardedTransport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("Ocean Engine SDK redirects are forbidden")
		},
	}
	configuration := sdkconfig.NewConfiguration()
	configuration.Host = profile.Host
	configuration.Scheme = profile.Scheme
	configuration.HTTPClient = httpClient
	configuration.LogEnable = false
	configuration.UseLogMw = false
	configuration.Debug = false
	profile.TimeoutProfile = timeoutProfile
	profile.Timeout = timeout
	profile.MaxResponseBytes = maxResponseBytes
	return &Client{sdk: sdk.Init(configuration), profile: profile}
}

func newTransport() *http.Transport {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment, DialContext: dialer.DialContext,
		ForceAttemptHTTP2: true, MaxIdleConns: 32, MaxIdleConnsPerHost: 16,
		IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 5 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second, ExpectContinueTimeout: time.Second,
	}
}

func (client *Client) Profile() ClientProfile {
	return client.profile
}

type governedTransport struct {
	base                   http.RoundTripper
	channel                string
	governor               *requestcontrol.Governor
	sharedQianchuanControl requestcontrol.SharedController
}

func (transport *governedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || request.Context() == nil {
		return nil, &notDispatchedError{cause: errors.New("Ocean Engine SDK request is incomplete")}
	}
	if err := request.Context().Err(); err != nil {
		return nil, &notDispatchedError{cause: err}
	}
	scope, ok := requestcontrol.Authorization(request.Context())
	if !ok || scope.Channel != transport.channel {
		return nil, &notDispatchedError{cause: requestcontrol.ErrAuthorizationScopeMissing}
	}
	endpoint, err := endpointFamily(request.URL.Path)
	if err != nil {
		return nil, &notDispatchedError{cause: err}
	}
	release, err := transport.governor.Acquire(request.Context(), scope, endpoint)
	if err != nil {
		return nil, &notDispatchedError{cause: err}
	}
	var sharedRelease func(error, *http.Response) error
	if transport.channel == "qianchuan" && transport.sharedQianchuanControl != nil {
		advertiserID, ok := requestcontrol.Advertiser(request.Context())
		if !ok {
			release()
			return nil, &notDispatchedError{cause: requestcontrol.ErrAdvertiserScopeMissing}
		}
		sharedRelease, err = transport.sharedQianchuanControl.Acquire(request.Context(), advertiserID)
		if err != nil {
			release()
			return nil, &notDispatchedError{cause: err}
		}
	}
	if err := requestcontrol.ReserveAttempt(request.Context()); err != nil {
		if sharedRelease != nil {
			_ = sharedRelease(err, nil)
		}
		release()
		return nil, &notDispatchedError{cause: err}
	}
	response, requestErr := transport.base.RoundTrip(request)
	if sharedRelease == nil {
		if requestErr != nil || response == nil || response.Body == nil {
			release()
			return response, requestErr
		}
		response.Body = &releaseOnCloseBody{ReadCloser: response.Body, release: release}
		return response, nil
	}
	if requestErr != nil || response == nil || response.Body == nil {
		if releaseErr := sharedRelease(requestErr, response); requestErr == nil && releaseErr != nil {
			requestErr = releaseErr
		}
		release()
		return response, requestErr
	}
	response.Body = &sharedControlledBody{body: response.Body, response: response, release: sharedRelease}
	response.Body = &releaseOnCloseBody{ReadCloser: response.Body, release: release}
	return response, nil
}

type releaseOnCloseBody struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (body *releaseOnCloseBody) Close() error {
	err := body.ReadCloser.Close()
	body.once.Do(body.release)
	return err
}

type sharedControlledBody struct {
	body     io.ReadCloser
	response *http.Response
	release  func(error, *http.Response) error
	buffer   bytes.Buffer
	once     sync.Once
	err      error
}

func (body *sharedControlledBody) Read(target []byte) (int, error) {
	count, err := body.body.Read(target)
	if body.buffer.Len() < qianchuanEnvelopePeek && count > 0 {
		remaining := qianchuanEnvelopePeek - body.buffer.Len()
		body.buffer.Write(target[:min(count, remaining)])
	}
	return count, err
}

func (body *sharedControlledBody) Close() error {
	if body.buffer.Len() < qianchuanEnvelopePeek {
		_, _ = io.CopyN(
			&body.buffer,
			body.body,
			int64(qianchuanEnvelopePeek-body.buffer.Len()),
		)
	}
	closeErr := body.body.Close()
	body.once.Do(func() {
		requestErr := closeErr
		var envelope struct {
			Code json.RawMessage `json:"code"`
		}
		if json.Unmarshal(body.buffer.Bytes(), &envelope) == nil && qianchuanRateLimitCode(envelope.Code) {
			requestErr = errors.New("Ocean Engine API business error 40100")
		}
		body.err = body.release(requestErr, body.response)
	})
	if closeErr != nil {
		return closeErr
	}
	return body.err
}

func qianchuanRateLimitCode(value json.RawMessage) bool {
	var numeric int64
	if json.Unmarshal(value, &numeric) == nil {
		return numeric == 40100
	}
	var text string
	return json.Unmarshal(value, &text) == nil && text == "40100"
}

func endpointFamily(path string) (string, error) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "/open_api/") || strings.Contains(path, "..") {
		return "", errors.New("Ocean Engine SDK endpoint family is invalid")
	}
	path = strings.TrimSuffix(strings.TrimPrefix(path, "/open_api"), "/")
	if path == "" || len(path) > 512 {
		return "", errors.New("Ocean Engine SDK endpoint family is invalid")
	}
	return path, nil
}

type securityTransport struct {
	base             http.RoundTripper
	host             string
	scheme           string
	maxResponseBytes int64
}

func (transport *securityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request == nil || request.URL == nil || request.URL.Scheme != transport.scheme ||
		request.URL.Host != transport.host || request.URL.User != nil {
		return nil, errors.New("Ocean Engine SDK request escaped the approved host profile")
	}
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		var blocked *notDispatchedError
		return nil, &SDKRequestError{cause: err, dispatched: !errors.As(err, &blocked)}
	}
	if response == nil {
		return nil, &SDKRequestError{cause: errors.New("transport returned no response"), dispatched: true}
	}
	contentLength := response.ContentLength
	if contentLength > transport.maxResponseBytes {
		_ = response.Body.Close()
		return nil, fmt.Errorf("Ocean Engine SDK response exceeds %d bytes", transport.maxResponseBytes)
	}
	response.Body = &boundedBody{
		reader: io.LimitReader(response.Body, transport.maxResponseBytes+1),
		closer: response.Body, limit: transport.maxResponseBytes,
	}
	return response, nil
}

type boundedBody struct {
	reader io.Reader
	closer io.Closer
	limit  int64
	read   int64
}

func (body *boundedBody) Read(target []byte) (int, error) {
	count, err := body.reader.Read(target)
	body.read += int64(count)
	if body.read > body.limit {
		return count, errors.New("Ocean Engine SDK response body exceeded its limit")
	}
	return count, err
}

func (body *boundedBody) Close() error { return body.closer.Close() }

func Redact(message string) string {
	result := message
	for _, marker := range []string{"access_token", "refresh_token", "Access-Token", "Authorization", "secret", "auth_code"} {
		if strings.Contains(strings.ToLower(result), strings.ToLower(marker)) {
			return "sensitive Ocean Engine error details redacted"
		}
	}
	return result
}
