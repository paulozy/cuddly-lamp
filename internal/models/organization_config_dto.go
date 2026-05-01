package models

type OrganizationInfo struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Slug string   `json:"slug"`
	Role UserRole `json:"role"`
}

type OrganizationConfigResponse struct {
	AnthropicAPIKeyConfigured bool   `json:"anthropic_api_key_configured"`
	AnthropicTokensPerHour    int    `json:"anthropic_tokens_per_hour"`
	GithubTokenConfigured     bool   `json:"github_token_configured"`
	GitHubPRReviewEnabled     bool   `json:"github_pr_review_enabled"`
	WebhookBaseURL            string `json:"webhook_base_url,omitempty"`

	EmbeddingsProvider     string `json:"embeddings_provider"`
	VoyageAPIKeyConfigured bool   `json:"voyage_api_key_configured"`
	EmbeddingsModel        string `json:"embeddings_model"`
	EmbeddingsDimensions   int    `json:"embeddings_dimensions"`

	GitHubClientIDConfigured     bool   `json:"github_client_id_configured"`
	GitHubClientSecretConfigured bool   `json:"github_client_secret_configured"`
	GitHubCallbackURL            string `json:"github_callback_url,omitempty"`
	GitLabClientIDConfigured     bool   `json:"gitlab_client_id_configured"`
	GitLabClientSecretConfigured bool   `json:"gitlab_client_secret_configured"`
	GitLabCallbackURL            string `json:"gitlab_callback_url,omitempty"`

	// OutputLanguage is the BCP 47 tag used for AI-generated prose
	// (e.g. "en", "pt-BR"). Defaults to "en".
	OutputLanguage string `json:"output_language"`
}

type UpdateOrganizationConfigRequest struct {
	AnthropicAPIKey        *string `json:"anthropic_api_key"`
	AnthropicTokensPerHour *int    `json:"anthropic_tokens_per_hour"`
	GithubToken            *string `json:"github_token"`
	GitHubPRReviewEnabled  *bool   `json:"github_pr_review_enabled"`
	WebhookBaseURL         *string `json:"webhook_base_url"`

	EmbeddingsProvider   *string `json:"embeddings_provider"`
	VoyageAPIKey         *string `json:"voyage_api_key"`
	EmbeddingsModel      *string `json:"embeddings_model"`
	EmbeddingsDimensions *int    `json:"embeddings_dimensions"`

	GitHubClientID     *string `json:"github_client_id"`
	GitHubClientSecret *string `json:"github_client_secret"`
	GitHubCallbackURL  *string `json:"github_callback_url"`
	GitLabClientID     *string `json:"gitlab_client_id"`
	GitLabClientSecret *string `json:"gitlab_client_secret"`
	GitLabCallbackURL  *string `json:"gitlab_callback_url"`

	// OutputLanguage is a BCP 47 tag (e.g. "en", "pt-BR"). Validated server-side
	// via golang.org/x/text/language. Empty string falls back to the default.
	OutputLanguage *string `json:"output_language"`
}

func OrganizationConfigToResponse(cfg *OrganizationConfig) OrganizationConfigResponse {
	cfg.ApplyDefaults()
	return OrganizationConfigResponse{
		AnthropicAPIKeyConfigured:    cfg.AnthropicAPIKey != "",
		AnthropicTokensPerHour:       cfg.AnthropicTokensPerHour,
		GithubTokenConfigured:        cfg.GithubToken != "",
		GitHubPRReviewEnabled:        cfg.GitHubPRReviewEnabled,
		WebhookBaseURL:               cfg.WebhookBaseURL,
		EmbeddingsProvider:           cfg.EmbeddingsProvider,
		VoyageAPIKeyConfigured:       cfg.VoyageAPIKey != "",
		EmbeddingsModel:              cfg.EmbeddingsModel,
		EmbeddingsDimensions:         cfg.EmbeddingsDimensions,
		GitHubClientIDConfigured:     cfg.GitHubClientID != "",
		GitHubClientSecretConfigured: cfg.GitHubClientSecret != "",
		GitHubCallbackURL:            cfg.GitHubCallbackURL,
		GitLabClientIDConfigured:     cfg.GitLabClientID != "",
		GitLabClientSecretConfigured: cfg.GitLabClientSecret != "",
		GitLabCallbackURL:            cfg.GitLabCallbackURL,
		OutputLanguage:               cfg.ResolvedOutputLanguage(),
	}
}
