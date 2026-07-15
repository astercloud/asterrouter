package controlplane

import (
	"errors"
	"time"
)

var (
	ErrAIJobProgressConflict = errors.New("ai job progress sequence conflicts with existing state")
	ErrAIJobProgressInvalid  = errors.New("ai job progress observation is invalid")
)

// ProviderProgressObservation contains provider-reported progress only. Core
// persists it as an immutable fact and does not expose arbitrary provider
// messages, which could contain prompts or credentials.
type ProviderProgressObservation struct {
	Sequence int64  `json:"sequence"`
	Percent  *int   `json:"percent,omitempty"`
	Stage    string `json:"stage,omitempty"`
}

type AIJobProgressEvent struct {
	ID               string    `json:"id"`
	JobID            string    `json:"job_id"`
	AttemptID        string    `json:"attempt_id"`
	ProviderTaskID   string    `json:"provider_task_id,omitempty"`
	ProviderSequence int64     `json:"provider_sequence"`
	Percent          *int      `json:"percent,omitempty"`
	Stage            string    `json:"stage,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}
