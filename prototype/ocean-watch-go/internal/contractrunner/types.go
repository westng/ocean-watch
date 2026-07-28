package contractrunner

import "encoding/json"

const CaptureSchemaVersion = 1

type CaseSpec struct {
	ID            string            `json:"id"`
	Argv          []string          `json:"argv"`
	Fixture       string            `json:"fixture,omitempty"`
	Stdin         string            `json:"stdin,omitempty"`
	Environment   map[string]string `json:"environment,omitempty"`
	TimeoutMS     int               `json:"timeout_ms,omitempty"`
	Normalizers   []string          `json:"normalizers,omitempty"`
	NetworkPolicy string            `json:"network_policy,omitempty"`
}

type FixtureManifest struct {
	SchemaVersion int        `json:"schema_version"`
	Cases         []CaseSpec `json:"cases"`
}

type FileState struct {
	Kind    string `json:"kind"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256,omitempty"`
	Content string `json:"content,omitempty"`
}

type CaseResult struct {
	ExitCode     int                  `json:"exit_code"`
	TimedOut     bool                 `json:"timed_out"`
	StdoutKind   string               `json:"stdout_kind"`
	StdoutText   string               `json:"stdout_text,omitempty"`
	StdoutJSON   json.RawMessage      `json:"stdout_json,omitempty"`
	Stderr       string               `json:"stderr"`
	Presentation string               `json:"presentation,omitempty"`
	BeforeFiles  map[string]FileState `json:"before_files"`
	AfterFiles   map[string]FileState `json:"after_files"`
}

type CapturedCase struct {
	Category        string     `json:"category"`
	Spec            CaseSpec   `json:"spec"`
	FixtureSnapshot string     `json:"fixture_snapshot,omitempty"`
	Result          CaseResult `json:"result"`
}

type Capture struct {
	SchemaVersion  int            `json:"schema_version"`
	Kind           string         `json:"kind"`
	GitSHA         string         `json:"git_sha"`
	Platform       string         `json:"platform"`
	ManifestSHA256 string         `json:"manifest_sha256"`
	CommandCount   int            `json:"command_count"`
	Cases          []CapturedCase `json:"cases"`
}

type Difference struct {
	Field    string `json:"field"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type ComparedCase struct {
	ID          string       `json:"id"`
	Category    string       `json:"category"`
	Passed      bool         `json:"passed"`
	Differences []Difference `json:"differences"`
}

type ComparisonReport struct {
	SchemaVersion     int                `json:"schema_version"`
	Kind              string             `json:"kind"`
	GitSHA            string             `json:"git_sha"`
	Platform          string             `json:"platform"`
	ManifestSHA256    string             `json:"manifest_sha256"`
	CandidateIdentity *CandidateIdentity `json:"candidate_identity,omitempty"`
	Total             int                `json:"total"`
	Passed            int                `json:"passed"`
	Failed            int                `json:"failed"`
	Cases             []ComparedCase     `json:"cases"`
}

type Program struct {
	Executable string
	Prefix     []string
	Env        map[string]string
}

type CaptureOptions struct {
	ManifestPath   string
	FixturesPath   string
	OutputPath     string
	Program        Program
	RepositoryRoot string
	GitSHA         string
}

type CompareOptions struct {
	ManifestPath      string
	BaselinePath      string
	Candidate         Program
	OutputPath        string
	RepositoryRoot    string
	GitSHA            string
	CandidateIdentity *CandidateIdentity
}
