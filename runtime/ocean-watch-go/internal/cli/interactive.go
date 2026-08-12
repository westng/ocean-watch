package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

type promptReader struct {
	scanner *bufio.Scanner
	output  io.Writer
}

func newPromptReader(input io.Reader, output io.Writer) *promptReader {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	return &promptReader{scanner: scanner, output: output}
}

func (reader *promptReader) line(prompt string) (string, error) {
	value, err := reader.opaqueLine(prompt)
	return strings.TrimSpace(value), err
}

func (reader *promptReader) opaqueLine(prompt string) (string, error) {
	if _, err := io.WriteString(reader.output, prompt); err != nil {
		return "", err
	}
	if !reader.scanner.Scan() {
		if err := reader.scanner.Err(); err != nil {
			return "", err
		}
		return "", io.ErrUnexpectedEOF
	}
	return reader.scanner.Text(), nil
}

func (reader *promptReader) value(label string, defaultValue any, required bool) (string, error) {
	suffix := ""
	if !emptyPromptDefault(defaultValue) {
		suffix = " [" + promptString(defaultValue) + "]"
	}
	for {
		value, err := reader.line(label + suffix + ": ")
		if err != nil {
			return "", err
		}
		if value != "" {
			return value, nil
		}
		if !emptyPromptDefault(defaultValue) {
			return promptString(defaultValue), nil
		}
		if !required {
			return "", nil
		}
	}
}

func (reader *promptReader) opaqueValue(label string, defaultValue any) (string, error) {
	suffix := ""
	if !emptyPromptDefault(defaultValue) {
		suffix = " [" + promptString(defaultValue) + "]"
	}
	value, err := reader.opaqueLine(label + suffix + ": ")
	if err != nil {
		return "", err
	}
	if value != "" {
		return value, nil
	}
	if !emptyPromptDefault(defaultValue) {
		return promptString(defaultValue), nil
	}
	return "", nil
}

func (reader *promptReader) yesNo(label string, defaultValue bool) (bool, error) {
	hint := "y/N"
	if defaultValue {
		hint = "Y/n"
	}
	value, err := reader.line(fmt.Sprintf("%s [%s]: ", label, hint))
	if err != nil {
		return false, err
	}
	if value == "" {
		return defaultValue, nil
	}
	switch strings.ToLower(value) {
	case "y", "yes", "是":
		return true, nil
	default:
		return false, nil
	}
}

func emptyPromptDefault(value any) bool {
	return value == nil || promptString(value) == ""
}

func promptString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

type orderedField struct {
	name  string
	value any
}

type orderedObject []orderedField

func (object orderedObject) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for index, field := range object {
		if index != 0 {
			buffer.WriteByte(',')
		}
		name, err := marshalCompactJSON(field.name)
		if err != nil {
			return nil, err
		}
		value, err := marshalCompactJSON(field.value)
		if err != nil {
			return nil, err
		}
		buffer.Write(name)
		buffer.WriteByte(':')
		buffer.Write(value)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func marshalCompactJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

func writePrettyJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func orderedMap(value map[string]any, keys []string, transform func(string, any) any) orderedObject {
	result := make(orderedObject, 0, len(value))
	seen := make(map[string]bool, len(keys))
	for _, key := range keys {
		item, exists := value[key]
		if !exists {
			continue
		}
		seen[key] = true
		if transform != nil {
			item = transform(key, item)
		}
		result = append(result, orderedField{name: key, value: item})
	}
	remaining := make([]string, 0, len(value)-len(result))
	for key := range value {
		if !seen[key] {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	for _, key := range remaining {
		item := value[key]
		if transform != nil {
			item = transform(key, item)
		}
		result = append(result, orderedField{name: key, value: item})
	}
	return result
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func listValue(value any) []any {
	result, _ := value.([]any)
	if result == nil {
		return []any{}
	}
	return result
}

func textValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func copyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = copyValue(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = copyValue(item)
		}
		return result
	default:
		return typed
	}
}

func copyMap(value any) map[string]any {
	return copyValue(mapValue(value)).(map[string]any)
}

func deepMergeMaps(base, override map[string]any) map[string]any {
	result := copyMap(base)
	for key, value := range override {
		baseMap, baseOK := result[key].(map[string]any)
		overrideMap, overrideOK := value.(map[string]any)
		if baseOK && overrideOK {
			result[key] = deepMergeMaps(baseMap, overrideMap)
		} else {
			result[key] = copyValue(value)
		}
	}
	return result
}

func firstListText(value any) string {
	values := listValue(value)
	if len(values) == 0 {
		return ""
	}
	return textValue(values[0])
}

func unexpectedInput(err error) error {
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return errors.New("interactive input ended before the wizard completed")
	}
	return err
}
