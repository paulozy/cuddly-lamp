package models

import "time"

type Organization struct {
	ID          string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	Name        string     `gorm:"type:varchar(255);not null" json:"name"`
	Slug        string     `gorm:"type:varchar(120);uniqueIndex;not null" json:"slug"`
	Description string     `gorm:"type:text" json:"description,omitempty"`
	IsActive    bool       `gorm:"default:true" json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `gorm:"index" json:"deleted_at,omitempty"`
}

func (Organization) TableName() string {
	return "organizations"
}

func (o *Organization) IsValid() bool {
	return o.Name != "" && o.Slug != ""
}

type OrganizationMember struct {
	OrganizationID string       `gorm:"type:uuid;primaryKey" json:"organization_id"`
	UserID         string       `gorm:"type:uuid;primaryKey" json:"user_id"`
	Role           UserRole     `gorm:"type:varchar(50);not null;default:'developer'" json:"role"`
	IsActive       bool         `gorm:"default:true" json:"is_active"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
	Organization   Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	User           User         `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (OrganizationMember) TableName() string {
	return "organization_members"
}

type OrganizationConfig struct {
	ID             string `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	OrganizationID string `gorm:"type:uuid;uniqueIndex;not null" json:"organization_id"`

	AnthropicAPIKey        string `gorm:"type:bytea;serializer:enc" json:"-"`
	AnthropicTokensPerHour int    `gorm:"default:20000" json:"anthropic_tokens_per_hour"`
	GithubToken            string `gorm:"type:bytea;serializer:enc" json:"-"`
	GitHubPRReviewEnabled  bool   `gorm:"default:false" json:"github_pr_review_enabled"`
	WebhookBaseURL         string `gorm:"type:text" json:"webhook_base_url,omitempty"`

	EmbeddingsProvider   string `gorm:"type:varchar(50);default:'voyage'" json:"embeddings_provider"`
	VoyageAPIKey         string `gorm:"type:bytea;serializer:enc" json:"-"`
	EmbeddingsModel      string `gorm:"type:varchar(100);default:'voyage-code-3'" json:"embeddings_model"`
	EmbeddingsDimensions int    `gorm:"default:1024" json:"embeddings_dimensions"`

	GitHubClientID     string `gorm:"type:varchar(255)" json:"github_client_id,omitempty"`
	GitHubClientSecret string `gorm:"type:bytea;serializer:enc" json:"-"`
	GitHubCallbackURL  string `gorm:"type:text" json:"github_callback_url,omitempty"`
	GitLabClientID     string `gorm:"type:varchar(255)" json:"gitlab_client_id,omitempty"`
	GitLabClientSecret string `gorm:"type:bytea;serializer:enc" json:"-"`
	GitLabCallbackURL  string `gorm:"type:text" json:"gitlab_callback_url,omitempty"`

	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	Organization Organization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
}

func (OrganizationConfig) TableName() string {
	return "organization_configs"
}

func (c *OrganizationConfig) ApplyDefaults() {
	if c.AnthropicTokensPerHour == 0 {
		c.AnthropicTokensPerHour = 20000
	}
	if c.EmbeddingsProvider == "" {
		c.EmbeddingsProvider = "voyage"
	}
	if c.EmbeddingsModel == "" {
		c.EmbeddingsModel = "voyage-code-3"
	}
	if c.EmbeddingsDimensions == 0 {
		c.EmbeddingsDimensions = 1024
	}
}
