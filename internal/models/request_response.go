package models

// JobResponse represents a queued job
type JobResponse struct {
	Status string `json:"status"` // queued, processing, completed, failed
	Type   string `json:"type"`   // job type (e.g., docs:generate)
	Target string `json:"target"` // resource being processed
}
