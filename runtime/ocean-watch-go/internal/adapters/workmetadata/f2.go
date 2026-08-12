package workmetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	pythonruntime "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/python"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
	portworkmetadata "github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/ports/workmetadata"
)

const (
	DefaultF2Timeout   = 25 * time.Second
	MaxF2ResponseBytes = 1 << 20
	F2EntrypointEnv    = "OCEAN_WATCH_F2_ENTRYPOINT"
)

type F2Resolver struct {
	Python     pythonruntime.Resolver
	Entrypoint string
	Directory  string
	Timeout    time.Duration
}

type f2Envelope struct {
	Results     map[string]json.RawMessage                `json:"results"`
	Errors      map[string]portworkmetadata.MetadataError `json:"errors"`
	Performance map[string]any                            `json:"performance"`
	Error       *struct {
		Code string `json:"code"`
	} `json:"error"`
}

type f2Metadata struct {
	Code int `json:"code"`
	Data struct {
		Author struct {
			Nickname string `json:"nickname"`
			UniqueID string `json:"unique_id"`
			UID      string `json:"uid"`
		} `json:"author"`
		Product struct {
			ID   string `json:"product_info_id"`
			Name string `json:"product_info_name"`
		} `json:"product"`
		Video struct {
			ID string `json:"video_info_id"`
		} `json:"video"`
	} `json:"data"`
}

func (resolver F2Resolver) ResolveMany(
	ctx context.Context,
	workIDs []string,
	concurrency int,
) (portworkmetadata.MetadataResult, error) {
	if len(workIDs) == 0 {
		return portworkmetadata.MetadataResult{Rows: map[string]portworkmetadata.MetadataRow{}, Errors: map[string]portworkmetadata.MetadataError{}}, nil
	}
	if concurrency < 1 || concurrency > 10 {
		return portworkmetadata.MetadataResult{}, errors.New("F2 concurrency must be between 1 and 10")
	}
	seen := map[string]bool{}
	arguments := []string{resolver.entrypoint(), "--concurrency", fmt.Sprint(concurrency)}
	for _, workID := range workIDs {
		if err := domain.ValidateDecimalID(workID, "aweme_item_id"); err != nil {
			return portworkmetadata.MetadataResult{}, err
		}
		if !seen[workID] {
			seen[workID] = true
			arguments = append(arguments, "--work-id", workID)
		}
	}
	runtime, err := resolver.Python.Resolve(ctx)
	if err != nil || !runtime.Version.AtLeast(pythonruntime.Version{Major: 3, Minor: 10}) {
		return portworkmetadata.MetadataResult{}, domain.NewWorkLinkError(
			"f2_runtime_unavailable", "F2 作品解析需要 Python 3.10+ 和固定版本 F2 "+pythonruntime.RequiredF2Version,
		)
	}
	timeout := resolver.Timeout
	if timeout <= 0 {
		timeout = DefaultF2Timeout
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(commandContext, runtime.Executable, append(runtime.Prefix, arguments...)...)
	if directory := strings.TrimSpace(resolver.Directory); directory != "" {
		command.Dir = directory
	}
	command.Stdin = bytes.NewReader(nil)
	var stdout bytes.Buffer
	stdoutWriter := &limitedBuffer{buffer: &stdout, limit: MaxF2ResponseBytes}
	command.Stdout = stdoutWriter
	command.Stderr = &limitedBuffer{buffer: new(bytes.Buffer), limit: 64 << 10}
	runErr := command.Run()
	if runErr != nil && commandContext.Err() != nil {
		return portworkmetadata.MetadataResult{}, domain.NewWorkLinkError("f2_cli_timeout", "F2 作品解析超时")
	}
	if stdoutWriter.overflow {
		return portworkmetadata.MetadataResult{}, domain.NewWorkLinkError("f2_response_too_large", "F2 作品解析响应超过大小限制")
	}
	var envelope f2Envelope
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return portworkmetadata.MetadataResult{}, domain.NewWorkLinkError("invalid_f2_response", "F2 作品解析未返回有效 JSON")
	}
	if envelope.Error != nil {
		return portworkmetadata.MetadataResult{}, domain.NewWorkLinkError("f2_runtime_unavailable", "F2 作品解析运行环境不可用")
	}
	if runErr != nil && len(envelope.Results) == 0 && len(envelope.Errors) == 0 {
		return portworkmetadata.MetadataResult{}, domain.NewWorkLinkError("f2_cli_failed", "F2 作品解析未返回可用结果")
	}
	result := portworkmetadata.MetadataResult{
		Rows: map[string]portworkmetadata.MetadataRow{}, Errors: envelope.Errors,
		Performance: safeF2Performance(envelope.Performance),
	}
	for workID, raw := range envelope.Results {
		if !seen[workID] {
			continue
		}
		var metadata f2Metadata
		if json.Unmarshal(raw, &metadata) != nil || metadata.Code != 200 || metadata.Data.Video.ID != workID ||
			!positiveDecimal(metadata.Data.Author.UID) || strings.TrimSpace(metadata.Data.Author.UniqueID) == "" {
			continue
		}
		productID := strings.TrimSpace(metadata.Data.Product.ID)
		if productID != "" && !positiveDecimal(productID) {
			productID = ""
		}
		result.Rows[workID] = portworkmetadata.MetadataRow{
			AwemeItemID: workID, CreatorName: strings.TrimSpace(metadata.Data.Author.Nickname),
			AwemeID: metadata.Data.Author.UID, AwemeShowID: strings.TrimSpace(metadata.Data.Author.UniqueID),
			ProductID: productID, ProductName: strings.TrimSpace(metadata.Data.Product.Name),
			Metadata: append([]byte(nil), raw...),
		}
	}
	for _, workID := range workIDs {
		if _, ok := result.Rows[workID]; !ok {
			if _, ok := result.Errors[workID]; !ok {
				result.Errors[workID] = portworkmetadata.MetadataError{Code: "f2_metadata_query_failed", Message: "F2 未返回可用的公开作品元数据"}
			}
		}
	}
	return result, nil
}

func (resolver F2Resolver) entrypoint() string {
	if value := strings.TrimSpace(resolver.Entrypoint); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv(F2EntrypointEnv)); value != "" {
		return value
	}
	if directory := strings.TrimSpace(resolver.Directory); directory != "" {
		return filepath.Join(directory, "f2", "resolve.py")
	}
	executable, err := os.Executable()
	if err == nil {
		for current := filepath.Dir(executable); current != filepath.Dir(current); current = filepath.Dir(current) {
			candidate := filepath.Join(current, "f2", "resolve.py")
			if info, statErr := os.Stat(candidate); statErr == nil && info.Mode().IsRegular() {
				return candidate
			}
		}
	}
	return filepath.Join("f2", "resolve.py")
}

type limitedBuffer struct {
	buffer   *bytes.Buffer
	limit    int
	overflow bool
}

func (writer *limitedBuffer) Write(value []byte) (int, error) {
	if len(value) > writer.limit-writer.buffer.Len() {
		writer.overflow = true
	}
	if writer.buffer.Len() >= writer.limit {
		return len(value), nil
	}
	remaining := writer.limit - writer.buffer.Len()
	_, _ = writer.buffer.Write(value[:min(len(value), remaining)])
	return len(value), nil
}

func positiveDecimal(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "0" && value[0] != '0' && domain.ValidateDecimalID(value, "id") == nil
}

func safeF2Performance(value map[string]any) map[string]any {
	result := map[string]any{}
	for _, key := range []string{
		"requested_count", "success_count", "failure_count", "concurrency", "first_pass_seconds",
		"retry_count", "retry_seconds", "total_seconds", "deadline_seconds", "timed_out_count",
		"slowest_work_id", "slowest_seconds",
	} {
		if item, ok := value[key]; ok {
			result[key] = item
		}
	}
	return result
}
