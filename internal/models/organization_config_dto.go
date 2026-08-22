package models

type OrganizationInfo struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Slug string   `json:"slug"`
	Role UserRole `json:"role"`
}

type OrganizationConfigResponse struct {
	AnthropicAPIKeyConfigured bool `json:"anthropic_api_key_configured"`
	AnthropicTokensPerHour    int  `json:"anthropic_tokens_per_hour"`
	GithubTokenConfigured     bool `json:"github_token_configured"`
	GitlabTokenConfigured     bool `json:"gitlab_token_configured"`
	// GitlabBaseURL is the API root for self-hosted GitLab. Empty means
	// gitlab.com, which is what nearly every organization wants.
	GitlabBaseURL string `json:"gitlab_base_url,omitempty"`

	GitHubClientIDConfigured     bool   `json:"github_client_id_configured"`
	GitHubClientSecretConfigured bool   `json:"github_client_secret_configured"`
	GitHubCallbackURL            string `json:"github_callback_url,omitempty"`
	GitLabClientIDConfigured     bool   `json:"gitlab_client_id_configured"`
	GitLabClientSecretConfigured bool   `json:"gitlab_client_secret_configured"`
	GitLabCallbackURL            string `json:"gitlab_callback_url,omitempty"`

	// OutputLanguage is the BCP 47 tag used for generated documentation prose
	// (e.g. "en", "pt-BR"). Defaults to "en".
	OutputLanguage string `json:"output_language"`
}

type UpdateOrganizationConfigRequest struct {
	AnthropicAPIKey        *string `json:"anthropic_api_key"`
	AnthropicTokensPerHour *int    `json:"anthropic_tokens_per_hour"`
	GithubToken            *string `json:"github_token"`
	GitlabToken            *string `json:"gitlab_token"`
	GitlabBaseURL          *string `json:"gitlab_base_url"`

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
		GitlabTokenConfigured:        cfg.GitlabToken != "",
		GitlabBaseURL:                cfg.GitlabBaseURL,
		GitHubClientIDConfigured:     cfg.GitHubClientID != "",
		GitHubClientSecretConfigured: cfg.GitHubClientSecret != "",
		GitHubCallbackURL:            cfg.GitHubCallbackURL,
		GitLabClientIDConfigured:     cfg.GitLabClientID != "",
		GitLabClientSecretConfigured: cfg.GitLabClientSecret != "",
		GitLabCallbackURL:            cfg.GitLabCallbackURL,
		OutputLanguage:               cfg.ResolvedOutputLanguage(),
	}
}
