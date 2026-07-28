package contractrunner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultCaseTimeout = 30 * time.Second

var (
	qianchuanProductTemplateIDPattern = regexp.MustCompile(`\bqcpt_[0-9a-f]{12}\b`)
	qianchuanLiveTemplateIDPattern    = regexp.MustCompile(`\bqclt_[0-9a-f]{12}\b`)
	quotedPythonLauncherPattern       = regexp.MustCompile(`^"[^"]+" "[^"]*[\\/]skills[\\/]ads-plan-monitor[\\/]run\.py"(?: |$)`)
)

func executeCase(parent context.Context, program Program, spec CaseSpec, fixtureSource string) (CaseResult, error) {
	workspace, err := os.MkdirTemp("", "ocean-watch-contract-")
	if err != nil {
		return CaseResult{}, err
	}
	defer os.RemoveAll(workspace)
	if fixtureSource != "" {
		if err := copyDirectory(fixtureSource, workspace); err != nil {
			return CaseResult{}, fmt.Errorf("copy fixture: %w", err)
		}
	}
	before, err := snapshotTree(workspace, spec.Normalizers)
	if err != nil {
		return CaseResult{}, err
	}
	arguments := make([]string, 0, len(program.Prefix)+len(spec.Argv))
	arguments = append(arguments, program.Prefix...)
	for _, argument := range spec.Argv {
		arguments = append(arguments, expandWorkspace(argument, workspace))
	}
	timeout := defaultCaseTimeout
	if spec.TimeoutMS > 0 {
		timeout = time.Duration(spec.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	command := exec.CommandContext(ctx, program.Executable, arguments...)
	command.Dir = workspace
	command.Stdin = strings.NewReader(expandWorkspace(spec.Stdin, workspace))
	command.Env = isolatedEnvironment(workspace, program.Env, spec.Environment)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	runErr := command.Run()
	exitCode := 0
	if runErr != nil {
		var exitError *exec.ExitError
		if errors.As(runErr, &exitError) {
			exitCode = exitError.ExitCode()
		} else if ctx.Err() != nil {
			exitCode = 130
		} else {
			return CaseResult{}, fmt.Errorf("start contract case %s: %w", spec.ID, runErr)
		}
	}
	after, err := snapshotTree(workspace, spec.Normalizers)
	if err != nil {
		return CaseResult{}, err
	}
	result, err := normalizeProcessResult(spec, workspace, exitCode, ctx.Err() == context.DeadlineExceeded, stdout.String(), stderr.String())
	if err != nil {
		return CaseResult{}, err
	}
	result.BeforeFiles = before
	result.AfterFiles = after
	return result, nil
}

func isolatedEnvironment(workspace string, program, fixture map[string]string) []string {
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	for key := range values {
		if strings.EqualFold(key, "PATH") {
			delete(values, key)
		}
	}
	home := filepath.Join(workspace, "home")
	values["HOME"] = home
	values["USERPROFILE"] = home
	values["CODEX_HOME"] = filepath.Join(workspace, "codex-home")
	values["PATH"] = filepath.Join(workspace, "empty-path")
	values["XDG_CONFIG_HOME"] = filepath.Join(workspace, "xdg-config")
	values["XDG_CACHE_HOME"] = filepath.Join(workspace, "xdg-cache")
	values["PYTHONDONTWRITEBYTECODE"] = "1"
	values["HTTP_PROXY"] = "http://127.0.0.1:1"
	values["HTTPS_PROXY"] = "http://127.0.0.1:1"
	values["ALL_PROXY"] = "http://127.0.0.1:1"
	values["NO_PROXY"] = ""
	for key, value := range program {
		values[key] = expandWorkspace(value, workspace)
	}
	for key, value := range fixture {
		values[key] = expandWorkspace(value, workspace)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func normalizeProcessResult(spec CaseSpec, workspace string, exitCode int, timedOut bool, stdout, stderr string) (CaseResult, error) {
	stdout = normalizeText(stdout, workspace, spec.Normalizers)
	stderr = normalizeText(stderr, workspace, spec.Normalizers)
	result := CaseResult{ExitCode: exitCode, TimedOut: timedOut, Stderr: stderr}
	var value any
	decoder := json.NewDecoder(strings.NewReader(stdout))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err == nil {
		var trailing any
		if err := decoder.Decode(&trailing); err == io.EOF {
			value = normalizeJSON(value, workspace, spec.Normalizers)
			canonical, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				return CaseResult{}, marshalErr
			}
			result.StdoutKind = "json"
			result.StdoutJSON = canonical
			if object, ok := value.(map[string]any); ok {
				if presentation, ok := object["presentation"].(map[string]any); ok {
					if rendered, ok := presentation["rendered_markdown"].(string); ok {
						result.Presentation = rendered
					}
				}
			}
			return result, nil
		}
	}
	result.StdoutKind = "text"
	result.StdoutText = stdout
	return result, nil
}

func normalizeText(value, workspace string, normalizers []string) string {
	result := strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	if workspace != "" {
		for _, variant := range workspaceVariants(workspace) {
			result = strings.ReplaceAll(result, variant, "{{WORKSPACE}}")
			result = strings.ReplaceAll(result, filepath.ToSlash(variant), "{{WORKSPACE}}")
		}
	}
	for _, normalizer := range normalizers {
		switch normalizer {
		case "trim-trailing-space":
			lines := strings.Split(result, "\n")
			for index := range lines {
				lines[index] = strings.TrimRight(lines[index], " \t")
			}
			result = strings.Join(lines, "\n")
		case "qianchuan-template-ids":
			result = qianchuanProductTemplateIDPattern.ReplaceAllString(result, "qcpt_{{DYNAMIC_ID}}")
			result = qianchuanLiveTemplateIDPattern.ReplaceAllString(result, "qclt_{{DYNAMIC_ID}}")
		case "launcher-command":
			result = normalizeLauncherCommand(result)
		}
	}
	return result
}

func workspaceVariants(workspace string) []string {
	variants := []string{workspace}
	if canonical, err := filepath.EvalSymlinks(workspace); err == nil && canonical != workspace {
		variants = append(variants, canonical)
	}
	sort.SliceStable(variants, func(left, right int) bool {
		return len(variants[left]) > len(variants[right])
	})
	return variants
}

func normalizeLauncherCommand(value string) string {
	const commandMarker = "{{OCEAN_WATCH_COMMAND}}"
	if value == "ocean-watch" {
		return commandMarker
	}
	if strings.HasPrefix(value, "ocean-watch ") {
		return commandMarker + value[len("ocean-watch"):]
	}
	if location := quotedPythonLauncherPattern.FindStringIndex(value); location != nil && location[0] == 0 {
		matched := value[:location[1]]
		suffix := value[location[1]:]
		if strings.HasSuffix(matched, " ") {
			suffix = " " + suffix
		}
		return commandMarker + suffix
	}
	return value
}

func normalizeJSON(value any, workspace string, normalizers []string) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			normalizedKey := normalizeText(key, workspace, normalizers)
			result[normalizedKey] = normalizeJSON(item, workspace, normalizers)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeJSON(item, workspace, normalizers)
		}
		return result
	case string:
		return normalizeText(typed, workspace, normalizers)
	default:
		return value
	}
}

func expandWorkspace(value, workspace string) string {
	return strings.ReplaceAll(value, "{{workspace}}", workspace)
}

func snapshotTree(root string, normalizers []string) (map[string]FileState, error) {
	result := map[string]FileState{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("contract workspace contains a symbolic link: %s", relative)
		}
		if info.IsDir() {
			result[relative] = FileState{Kind: "directory"}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("contract workspace contains an unsupported file: %s", relative)
		}
		payload, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		normalized := normalizeFile(relative, payload, normalizers)
		digest := sha256.Sum256(normalized)
		state := FileState{Kind: "file", Size: int64(len(normalized)), SHA256: hex.EncodeToString(digest[:])}
		if len(normalized) <= 1<<20 && !bytes.ContainsRune(normalized, '\x00') {
			state.Content = string(normalized)
		}
		result[relative] = state
		return nil
	})
	return result, err
}

func normalizeFile(relative string, payload []byte, normalizers []string) []byte {
	if strings.HasSuffix(relative, ".lock") {
		var metadata map[string]any
		if json.Unmarshal(payload, &metadata) == nil {
			if _, ok := metadata["pid"]; ok {
				metadata["pid"] = "{{PID}}"
			}
			if _, ok := metadata["nonce"]; ok {
				metadata["nonce"] = "{{NONCE}}"
			}
			payload, _ = json.Marshal(metadata)
		}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if decoder.Decode(&value) == nil {
		var trailing any
		if decoder.Decode(&trailing) == io.EOF {
			value = normalizeJSON(value, "", normalizers)
			if normalized, err := json.Marshal(value); err == nil {
				return normalized
			}
		}
	}
	return []byte(normalizeText(string(payload), "", normalizers))
}

func mapString(value any) string {
	payload, _ := json.Marshal(value)
	if len(payload) > 2000 {
		return strconv.Quote(string(payload[:2000]) + "…")
	}
	return string(payload)
}
