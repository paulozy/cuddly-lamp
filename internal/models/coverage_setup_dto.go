package models

import "time"

// CoverageSetupResponse is everything a person needs to wire their CI to the
// coverage endpoint, served by
// GET /api/v1/repositories/:id/coverage/setup.
//
// It carries *facts*, not a rendered snippet. The client composes the YAML,
// because the format selector and the report path are editable and round-tripping
// to the server on every keystroke would be absurd. What lives here is the
// knowledge the client cannot have: the platform's own public URL, whether that
// URL is even reachable from a CI runner, which CI the repository already uses,
// and whether any upload has ever arrived.
//
// The reason this endpoint exists at all: the settings panel used to print the
// *names* of three environment variables and never the one value only the
// platform knows. The feature shipped and stayed off, because finishing the setup
// required guessing.
type CoverageSetupResponse struct {
	// BaseURL is the platform's publicly reachable root, resolved as
	// organization override → platform configuration. Empty when neither is set.
	BaseURL string `json:"base_url,omitempty"`
	// IngestURL is the full URL a CI job posts to, repository id already in it,
	// so the client never has to assemble a path and get it wrong.
	IngestURL    string `json:"ingest_url,omitempty"`
	RepositoryID string `json:"repository_id"`

	// Reachable is false when BaseURL is empty or loopback. The client must then
	// render no snippet at all and say why: putting `http://localhost:3000` into
	// a CI file produces a step that fails on every run, which is the exact
	// silent breakage this endpoint exists to end. Judged by
	// services.WebhookRegistrationUnavailable — a CI runner and a provider
	// webhook have the same reachability requirement.
	Reachable bool `json:"reachable"`

	Provider RepositoryType `json:"provider"`

	// CISystem is the CI this repository already uses (`ci.github_actions`,
	// `ci.gitlab`, …), read back off the stored evidence path. Empty means
	// unknown, which is different from "has none".
	CISystem string `json:"ci_system,omitempty"`
	// CIConfigPath is the file that proved it, so the client can point at the
	// place the person needs to edit.
	CIConfigPath string `json:"ci_config_path,omitempty"`
	// HasCI is tri-state, as everywhere else this signal appears: nil means the
	// tree could not be fully inspected, never "no CI".
	HasCI *bool `json:"has_ci,omitempty"`
	// DefaultBranch is where a newly created CI file should land.
	DefaultBranch string `json:"default_branch,omitempty"`

	// Suggestion is a starting point, not a fact — see coverage.Suggestion. The
	// client keeps every field of it editable.
	Suggestion CoverageSuggestion `json:"suggestion"`
	// Formats is every format the ingest endpoint accepts, so the client's
	// selector cannot offer one that would come back 415.
	Formats []string `json:"formats"`

	// SecretEnvName is the *only* value that has to be a CI secret. The other two
	// inputs are a public URL and a UUID, and treating them as secrets is what
	// turned a one-step setup into a three-step one nobody finished.
	SecretEnvName string `json:"secret_env_name"`
	// Headers carries the real header names from the handler's own constants, so
	// a generated snippet cannot drift from the contract it posts to.
	Headers CoverageSetupHeaders `json:"headers"`

	// HasActiveToken and LastUploadAt are the feedback loop that was missing
	// entirely: a token nobody has used means the CI is not wired up, and until
	// now that state was invisible on both ends.
	HasActiveToken bool       `json:"has_active_token"`
	LastUploadAt   *time.Time `json:"last_upload_at,omitempty"`
}

// CoverageSuggestion mirrors coverage.Suggestion on the wire. It is restated here
// rather than embedded so `internal/models` keeps not importing sibling packages,
// which is the convention every other *_dto.go follows.
type CoverageSuggestion struct {
	Language    string `json:"language,omitempty"`
	Format      string `json:"format"`
	ReportPath  string `json:"report_path"`
	TestCommand string `json:"test_command,omitempty"`
}

// CoverageSetupHeaders names the headers the ingest endpoint reads.
type CoverageSetupHeaders struct {
	Format string `json:"format"`
	Commit string `json:"commit"`
	Branch string `json:"branch"`
}
