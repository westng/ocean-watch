package officialmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strings"
	"sync"
)

const maxProbeResponseBytes = 2 << 20

type captureContextKey struct{}

// Capture the result before the SDK's interface{} decoder can turn numbers
// into float64. This observes HTTP bodies, keeping the SDK's connection and
// private session-update hooks intact. Captures are per request and ephemeral.
type responseCapture struct {
	mu         sync.Mutex
	result     json.RawMessage
	invalid    bool
	httpStatus int
	retryAfter string
}

func (capture *responseCapture) accept(data, expectedID []byte) {
	var envelope struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(data, &envelope) != nil ||
		!bytes.Equal(bytes.TrimSpace(envelope.ID), expectedID) ||
		len(envelope.Result) == 0 {
		return
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.result) != 0 {
		capture.invalid = true
		return
	}
	capture.result = append(json.RawMessage{}, envelope.Result...)
}

func (capture *responseCapture) read() (json.RawMessage, *http.Response, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	response := &http.Response{StatusCode: capture.httpStatus, Header: http.Header{}}
	if capture.retryAfter != "" {
		response.Header.Set("Retry-After", capture.retryAfter)
	}
	if capture.invalid || len(capture.result) == 0 {
		return nil, response, ErrProbeInvalidResponse
	}
	return append(json.RawMessage{}, capture.result...), response, nil
}

type fixedTokenTransport struct {
	base      http.RoundTripper
	token     string
	toolRange string
	counts    *probeCounters
}

func (transport *fixedTokenTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "open.oceanengine.com" ||
		request.URL.Path != "/qianchuan/mcp" || request.URL.RawQuery != "" ||
		request.URL.Fragment != "" || request.URL.User != nil || request.URL.RawPath != "" {
		return nil, ErrProbeEndpoint
	}
	clone := request.Clone(request.Context())
	clone.Header.Set("Access-Token", transport.token)
	clone.Header.Set("Tool-Range", transport.toolRange)
	var rpc struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	if request.Method == http.MethodPost && request.GetBody != nil {
		body, err := request.GetBody()
		if err != nil {
			return nil, ErrProbeInvalidResponse
		}
		data, err := io.ReadAll(io.LimitReader(body, 64<<10))
		_ = body.Close()
		if err != nil || json.Unmarshal(data, &rpc) != nil {
			return nil, ErrProbeInvalidResponse
		}
		switch rpc.Method {
		case "initialize":
			transport.counts.initialize.Add(1)
		case "tools/list":
			transport.counts.list.Add(1)
		case "tools/call":
			transport.counts.call.Add(1)
		}
	}
	response, err := transport.base.RoundTrip(clone)
	if err != nil {
		return nil, err // The probe boundary replaces external error text.
	}
	capture, _ := request.Context().Value(captureContextKey{}).(*responseCapture)
	if capture == nil || len(rpc.ID) == 0 {
		return response, nil
	}
	capture.mu.Lock()
	capture.httpStatus = response.StatusCode
	capture.retryAfter = response.Header.Get("Retry-After")
	capture.mu.Unlock()
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (mediaType != "application/json" && mediaType != "text/event-stream") {
		return response, nil
	}
	response.Body = &captureBody{
		ReadCloser: response.Body,
		capture:    capture, expectedID: bytes.TrimSpace(rpc.ID),
		sse: mediaType == "text/event-stream",
	}
	return response, nil
}

type captureBody struct {
	io.ReadCloser
	mu         sync.Mutex
	capture    *responseCapture
	expectedID []byte
	sse        bool
	total      int
	pending    []byte
	event      []byte
	finished   bool
}

func (body *captureBody) Read(buffer []byte) (int, error) {
	n, err := body.ReadCloser.Read(buffer)
	body.mu.Lock()
	defer body.mu.Unlock()
	body.total += n
	if body.total > maxProbeResponseBytes {
		body.capture.mu.Lock()
		body.capture.invalid = true
		body.capture.mu.Unlock()
		return n, ErrProbeInvalidResponse
	}
	body.pending = append(body.pending, buffer[:n]...)
	if body.sse {
		for {
			index := bytes.IndexByte(body.pending, '\n')
			if index < 0 {
				break
			}
			line := strings.TrimSuffix(string(body.pending[:index]), "\r")
			body.pending = body.pending[index+1:]
			switch {
			case line == "":
				if len(body.event) != 0 {
					body.capture.accept(bytes.TrimSuffix(body.event, []byte("\n")), body.expectedID)
					body.event = nil
				}
			case strings.HasPrefix(line, "data:"):
				value := strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " ")
				body.event = append(body.event, value...)
				body.event = append(body.event, '\n')
			}
		}
	}
	if err == io.EOF {
		body.finishLocked()
	}
	return n, err
}

func (body *captureBody) Close() error {
	err := body.ReadCloser.Close()
	body.mu.Lock()
	body.finishLocked()
	body.mu.Unlock()
	return err
}

func (body *captureBody) finishLocked() {
	if body.finished {
		return
	}
	body.finished = true
	if !body.sse {
		body.capture.accept(body.pending, body.expectedID)
	}
}

func captureContext(ctx context.Context) (context.Context, *responseCapture) {
	capture := &responseCapture{}
	return context.WithValue(ctx, captureContextKey{}, capture), capture
}
