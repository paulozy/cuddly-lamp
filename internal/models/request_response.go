package models

// JobResponse represents a queued job
type JobResponse struct {
	Status string `json:"status"` // queued, processing, completed, failed
	Type   string `json:"type"`   // job type (e.g., docs:generate)
	Target string `json:"target"` // resource being processed
	// RetryAfterSeconds is set when the request was declined by a throttle, so a
	// client can say "again in 40s" instead of leaving the person to guess whether
	// the button works. Omitted when retrying is pointless or immediately fine.
	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
}
