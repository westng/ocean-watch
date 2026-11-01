// Package official defines the private, read-only backend boundary. Credentials
// and upstream payloads never form part of a Host input or result contract.
package official

import (
	"context"
	"encoding/json"
)

type Lease struct {
	AuthorizationID string `json:"-"`
	Revision        int    `json:"-"`
	Token           string `json:"-"`
}

type Tool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	SourceDigest string          `json:"source_digest"`
}

type Request struct {
	QueryID        string
	Profile        []string
	Lease          Lease
	ControlKey     string
	Arguments      map[string]json.RawMessage `json:"-"`
	ApprovedInput  json.RawMessage
	SourceInput    json.RawMessage
	ApprovedOutput json.RawMessage
}

type Response struct {
	Data          any `json:"-"`
	SourceDigest  string
	Compatibility string
	APICode       string
}

type Backend interface {
	Read(context.Context, Request) (Response, error)
}

type AppTokenLease struct {
	AppID string `json:"-"`
	Token string `json:"-"`
}
type AppTokenProvider interface {
	EnsureApp(context.Context, string) (AppTokenLease, error)
}

// Failure contains only controlled codes, never an upstream message or URL.
type Failure struct {
	Code            string
	Stage           string
	APICode         string
	Dispatched      bool
	AuthorizationID string
	AdvertiserID    string
	RequiredFields  []string
}

func (failure *Failure) Error() string { return failure.Code }
