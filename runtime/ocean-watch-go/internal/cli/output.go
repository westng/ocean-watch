package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"

	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/adapters/filesystem"
	"github.com/westng/ocean-watch/runtime/ocean-watch-go/internal/domain"
)

type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

type ErrorEnvelope struct {
	OK    bool      `json:"ok"`
	Error ErrorBody `json:"error"`
}

type AccountListEnvelope struct {
	OK           bool                    `json:"ok"`
	Accounts     []domain.ManagedAccount `json:"accounts"`
	Presentation domain.Presentation     `json:"presentation"`
}

type AccountMutationEnvelope struct {
	OK      bool                  `json:"ok"`
	Action  string                `json:"action"`
	Account domain.ManagedAccount `json:"account"`
}

type AccountReference struct {
	Channel      domain.Channel `json:"channel"`
	AdvertiserID string         `json:"advertiser_id"`
}

type AccountRemovalEnvelope struct {
	OK      bool             `json:"ok"`
	Action  string           `json:"action"`
	Account AccountReference `json:"account"`
}

type RunListEnvelope struct {
	Mode     string              `json:"mode"`
	RunCount int                 `json:"run_count"`
	Runs     []domain.RunSummary `json:"runs"`
}

type RunDetailEnvelope struct {
	Mode    string            `json:"mode"`
	Summary domain.RunSummary `json:"summary"`
	Run     domain.RunJournal `json:"run"`
}

func WriteJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func WriteJSONDestination(writer io.Writer, value any, destination string) error {
	var payload bytes.Buffer
	if err := WriteJSON(&payload, value); err != nil {
		return err
	}
	if destination != "" {
		path := filepath.Clean(destination)
		if err := filesystem.AtomicWritePrivateFile(path, payload.Bytes()); err != nil {
			return err
		}
	}
	_, err := writer.Write(payload.Bytes())
	return err
}

func WriteDomainError(writer io.Writer, err *domain.Error) {
	_ = WriteJSON(writer, ErrorEnvelope{OK: false, Error: ErrorBody{
		Code: err.Code, Message: err.Message, Details: err.Details,
	}})
}
