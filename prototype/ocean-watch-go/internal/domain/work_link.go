package domain

import "fmt"

type ResolvedWorkLink struct {
	InputIndex   int            `json:"input_index"`
	InputURL     string         `json:"input_url"`
	ResolvedURL  string         `json:"resolved_url"`
	CanonicalURL string         `json:"canonical_url"`
	AwemeItemID  string         `json:"aweme_item_id"`
	CreatorName  string         `json:"creator_name_hint,omitempty"`
	OwnerHint    *WorkOwnerHint `json:"owner_hint,omitempty"`
}

type WorkOwnerHint struct {
	AwemeID     string `json:"aweme_id"`
	AwemeShowID string `json:"aweme_show_id"`
}

type SkippedWorkLink struct {
	InputIndex   int    `json:"input_index"`
	InputURL     string `json:"input_url"`
	ResolvedURL  string `json:"resolved_url,omitempty"`
	CanonicalURL string `json:"canonical_url,omitempty"`
	AwemeItemID  string `json:"aweme_item_id,omitempty"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
	Message      string `json:"message"`
}

type WorkLinkError struct {
	Code    string
	Message string
}

func (err *WorkLinkError) Error() string {
	if err == nil {
		return ""
	}
	return err.Message
}

func NewWorkLinkError(code, message string) error {
	if code == "" || message == "" {
		return fmt.Errorf("invalid work-link error")
	}
	return &WorkLinkError{Code: code, Message: message}
}
