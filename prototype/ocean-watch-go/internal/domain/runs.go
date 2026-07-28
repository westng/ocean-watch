package domain

import (
	"encoding/json"
	"strconv"
	"strings"
)

type RunSummary struct {
	RunID         string
	Kind          string
	SchemaVersion any
	CreatedAt     any
	Fingerprint   any
	JobCount      int
	StatusCounts  map[string]int
	UpdatedAt     float64
	Readable      *bool
}

type PythonFloat64 float64

func (value PythonFloat64) MarshalJSON() ([]byte, error) {
	rendered := strconv.FormatFloat(float64(value), 'f', -1, 64)
	if !strings.Contains(rendered, ".") {
		rendered += ".0"
	}
	return []byte(rendered), nil
}

func (summary RunSummary) MarshalJSON() ([]byte, error) {
	if summary.Readable != nil {
		return json.Marshal(struct {
			RunID    string `json:"run_id"`
			Readable bool   `json:"readable"`
		}{RunID: summary.RunID, Readable: *summary.Readable})
	}
	return json.Marshal(struct {
		RunID         string         `json:"run_id"`
		Kind          string         `json:"kind"`
		SchemaVersion any            `json:"schema_version"`
		CreatedAt     any            `json:"created_at"`
		Fingerprint   any            `json:"fingerprint"`
		JobCount      int            `json:"job_count"`
		StatusCounts  map[string]int `json:"status_counts"`
		UpdatedAt     PythonFloat64  `json:"updated_at"`
	}{
		RunID: summary.RunID, Kind: summary.Kind, SchemaVersion: summary.SchemaVersion,
		CreatedAt: summary.CreatedAt, Fingerprint: summary.Fingerprint,
		JobCount: summary.JobCount, StatusCounts: summary.StatusCounts, UpdatedAt: PythonFloat64(summary.UpdatedAt),
	})
}

type RunJournal struct {
	Data map[string]any
}

func (journal RunJournal) MarshalJSON() ([]byte, error) {
	return json.Marshal(journal.Data)
}
