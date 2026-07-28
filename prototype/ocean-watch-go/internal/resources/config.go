package resources

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
)

//go:embed config.example.json
var defaultConfig []byte

func DefaultConfig() (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(defaultConfig))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("embedded default config must be an object")
	}
	return result, nil
}

func DefaultConfigBytes() []byte {
	return append([]byte(nil), defaultConfig...)
}
