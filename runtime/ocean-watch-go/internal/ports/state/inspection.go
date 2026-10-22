package state

import "context"

type Inspection struct {
	Status        string `json:"status"`
	SchemaVersion int    `json:"schema_version,omitempty"`
	Evidence      string `json:"evidence"`
	NextAction    string `json:"next_action"`
}

type Inspector interface {
	Inspect(context.Context) (Inspection, error)
}
