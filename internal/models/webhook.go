package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// StringArray maps a Go []string to a PostgreSQL text[] column.
type StringArray []string

func (a StringArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, s := range a {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(strings.ReplaceAll(s, `"`, `\"`))
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String(), nil
}

func (a *StringArray) Scan(src any) error {
	var raw string
	switch v := src.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	case nil:
		*a = StringArray{}
		return nil
	default:
		return fmt.Errorf("StringArray: unsupported source type %T", src)
	}
	raw = strings.TrimPrefix(strings.TrimSuffix(raw, "}"), "{")
	if raw == "" {
		*a = StringArray{}
		return nil
	}
	var result []string
	for _, part := range strings.Split(raw, ",") {
		result = append(result, strings.Trim(part, `"`))
	}
	*a = result
	return nil
}

type WebhookEventType string

const (
	// GitHub events
	WebhookEventPush        WebhookEventType = "push"
	WebhookEventPullRequest WebhookEventType = "pull_request"
	WebhookEventIssue       WebhookEventType = "issues"
	WebhookEventRelease     WebhookEventType = "release"
	WebhookEventRepository  WebhookEventType = "repository"
	WebhookEventWorkflowRun WebhookEventType = "workflow_run"

	// GitLab events
	WebhookEventMergeRequest WebhookEventType = "merge_request"
	WebhookEventPipeline     WebhookEventType = "pipeline"
	WebhookEventTag          WebhookEventType = "tag_push"

	// Custom
	WebhookEventUnknown WebhookEventType = "unknown"
)

type WebhookEventPayload struct {
	EventID   string    `json:"event_id,omitempty"`
	EventType string    `json:"event_type"`
	Provider  string    `json:"provider"` // github, gitlab, gitea
	Timestamp time.Time `json:"timestamp"`

	RepositoryID   string `json:"repository_id,omitempty"`
	RepositoryName string `json:"repository_name,omitempty"`

	PullRequestID     *int   `json:"pull_request_id,omitempty"`
	PullRequestNumber *int   `json:"pull_request_number,omitempty"`
	Branch            string `json:"branch,omitempty"`
	CommitSHA         string `json:"commit_sha,omitempty"`
	CommitMessage     string `json:"commit_message,omitempty"`

	ActorID   string `json:"actor_id,omitempty"`
	ActorName string `json:"actor_name,omitempty"`

	RawData map[string]interface{} `json:"raw_data,omitempty"`
}

func (wep *WebhookEventPayload) Scan(value interface{}) error {
	bytes, _ := value.([]byte)
	return json.Unmarshal(bytes, &wep)
}

func (wep WebhookEventPayload) Value() (driver.Value, error) {
	return json.Marshal(wep)
}

type WebhookProcessingResult struct {
	Success     bool      `json:"success"`
	ProcessedAt time.Time `json:"processed_at"`
	Error       string    `json:"error,omitempty"`

	// What was processed
	AnalysisID string `json:"analysis_id,omitempty"` // If triggered code analysis
	SyncID     string `json:"sync_id,omitempty"`     // If triggered sync
	ReviewID   string `json:"review_id,omitempty"`   // If triggered AI review

	ProcessingTimeMs int64 `json:"processing_time_ms"`
	TokensUsed       int   `json:"tokens_used,omitempty"`
}

func (wpr *WebhookProcessingResult) Scan(value interface{}) error {
	bytes, _ := value.([]byte)
	return json.Unmarshal(bytes, &wpr)
}

func (wpr WebhookProcessingResult) Value() (driver.Value, error) {
	return json.Marshal(wpr)
}

type Webhook struct {
	ID string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`

	RepositoryID string      `gorm:"type:uuid;index" json:"repository_id"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`

	EventType    WebhookEventType    `gorm:"type:varchar(50);index" json:"event_type"`
	EventPayload WebhookEventPayload `gorm:"type:jsonb" json:"event_payload"`

	Status           string                  `gorm:"type:varchar(50);default:'pending';index" json:"status"` // pending, processing, completed, failed
	ProcessingError  string                  `gorm:"type:text" json:"processing_error,omitempty"`
	ProcessingResult WebhookProcessingResult `gorm:"type:jsonb" json:"processing_result,omitempty"`

	DeliveryID  string     `gorm:"type:varchar(255);uniqueIndex" json:"delivery_id"` // GitHub/GitLab delivery ID
	RetryCount  int        `gorm:"default:0" json:"retry_count"`
	MaxRetries  int        `gorm:"default:3" json:"max_retries"`
	NextRetryAt *time.Time `json:"next_retry_at,omitempty"`
	FailedAt    *time.Time `json:"failed_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Webhook) TableName() string {
	return "webhooks"
}

func (w *Webhook) IsValid() bool {
	return w.RepositoryID != "" && w.EventType != "" && w.DeliveryID != ""
}

func (w *Webhook) ShouldRetry() bool {
	return w.Status == "failed" && w.RetryCount < w.MaxRetries
}

func (w *Webhook) CanProcess() bool {
	if w.Status != "pending" && w.Status != "failed" {
		return false
	}

	if w.NextRetryAt != nil && time.Now().Before(*w.NextRetryAt) {
		return false
	}

	return true
}

func (w *Webhook) MarkAsProcessing() {
	w.Status = "processing"
	w.UpdatedAt = time.Now()
}

func (w *Webhook) MarkAsCompleted(result WebhookProcessingResult) {
	w.Status = "completed"
	w.ProcessingResult = result
	w.ProcessingError = ""
	w.UpdatedAt = time.Now()
}

func (w *Webhook) MarkAsFailed(errMsg string) {
	w.Status = "failed"
	w.ProcessingError = errMsg
	w.RetryCount++
	w.UpdatedAt = time.Now()
	w.FailedAt = &w.UpdatedAt

	if w.ShouldRetry() {
		backoffMinutes := []int{1, 5, 15}
		if w.RetryCount-1 < len(backoffMinutes) {
			nextRetry := time.Now().Add(time.Duration(backoffMinutes[w.RetryCount-1]) * time.Minute)
			w.NextRetryAt = &nextRetry
		}
	}
}

type WebhookConfig struct {
	ID           string      `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	RepositoryID string      `gorm:"type:uuid;uniqueIndex" json:"repository_id"`
	Repository   *Repository `gorm:"foreignKey:RepositoryID" json:"repository,omitempty"`

	// Provider webhook settings
	WebhookURL string   `gorm:"type:text" json:"webhook_url"`
	Secret     string   `gorm:"type:bytea;serializer:enc" json:"-"`
	Events     StringArray `gorm:"type:text[];default:'{}'" json:"events"`
	IsActive   bool     `gorm:"default:true" json:"is_active"`

	ProviderWebhookID string `gorm:"type:varchar(255)" json:"provider_webhook_id,omitempty"`
	ProviderType      string `gorm:"type:varchar(50)" json:"provider_type"` // github, gitlab, gitea

	LastDeliveryAt  *time.Time `json:"last_delivery_at,omitempty"`
	SuccessfulCount int        `gorm:"default:0" json:"successful_count"`
	FailedCount     int        `gorm:"default:0" json:"failed_count"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (WebhookConfig) TableName() string {
	return "webhook_configs"
}

func (wc *WebhookConfig) IsHealthy() bool {
	total := wc.SuccessfulCount + wc.FailedCount
	if total == 0 {
		return true // New webhook
	}

	successRate := float64(wc.SuccessfulCount) / float64(total)
	return successRate >= 0.9 && wc.IsActive
}
