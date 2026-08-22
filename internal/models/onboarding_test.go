package models

import (
	"testing"
	"time"
)

func stepWithConfig(t *testing.T, kind OnboardingStepKind, cfg OnboardingStepConfig) *OnboardingStep {
	t.Helper()
	step := &OnboardingStep{Kind: kind, Title: "Passo"}
	if err := step.SetConfig(cfg); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	return step
}

func TestOnboardingStep_Validate(t *testing.T) {
	tests := []struct {
		name    string
		step    *OnboardingStep
		wantErr bool
	}{
		{
			name: "markdown with body",
			step: &OnboardingStep{Kind: OnboardingStepKindMarkdown, Title: "Bem-vindo", Body: "# Olá"},
		},
		{
			// An editorial step with nothing to say is a blank screen for the
			// newcomer, so it is rejected where it is authored.
			name:    "markdown without body",
			step:    &OnboardingStep{Kind: OnboardingStepKindMarkdown, Title: "Bem-vindo"},
			wantErr: true,
		},
		{
			name: "repository with reference",
			step: stepWithConfig(t, OnboardingStepKindRepository, OnboardingStepConfig{RepositoryID: "repo-1"}),
		},
		{
			name:    "repository without reference",
			step:    stepWithConfig(t, OnboardingStepKindRepository, OnboardingStepConfig{}),
			wantErr: true,
		},
		{
			name: "team with reference",
			step: stepWithConfig(t, OnboardingStepKindTeam, OnboardingStepConfig{TeamID: "team-1"}),
		},
		{
			name:    "team without reference",
			step:    stepWithConfig(t, OnboardingStepKindTeam, OnboardingStepConfig{}),
			wantErr: true,
		},
		{
			name: "doc with reference",
			step: stepWithConfig(t, OnboardingStepKindDoc, OnboardingStepConfig{DocGenerationID: "doc-1"}),
		},
		{
			name:    "doc without reference",
			step:    stepWithConfig(t, OnboardingStepKindDoc, OnboardingStepConfig{}),
			wantErr: true,
		},
		{
			name: "link with url",
			step: stepWithConfig(t, OnboardingStepKindLink, OnboardingStepConfig{URL: "https://wiki.example"}),
		},
		{
			name:    "link without url",
			step:    stepWithConfig(t, OnboardingStepKindLink, OnboardingStepConfig{Label: "Wiki"}),
			wantErr: true,
		},
		{
			name: "checklist with items",
			step: stepWithConfig(t, OnboardingStepKindChecklist, OnboardingStepConfig{
				Items: []OnboardingStepChecklistItem{{Text: "Instalar Go"}},
			}),
		},
		{
			name:    "checklist with an empty item",
			step:    stepWithConfig(t, OnboardingStepKindChecklist, OnboardingStepConfig{Items: []OnboardingStepChecklistItem{{Text: "  "}}}),
			wantErr: true,
		},
		{
			name:    "checklist with no items",
			step:    stepWithConfig(t, OnboardingStepKindChecklist, OnboardingStepConfig{}),
			wantErr: true,
		},
		{
			name: "contacts with people",
			step: stepWithConfig(t, OnboardingStepKindContacts, OnboardingStepConfig{
				People: []OnboardingStepContact{{UserID: "user-1", Area: "Acesso"}},
			}),
		},
		{
			name:    "contacts naming nobody",
			step:    stepWithConfig(t, OnboardingStepKindContacts, OnboardingStepConfig{People: []OnboardingStepContact{{Area: "Acesso"}}}),
			wantErr: true,
		},
		{
			name: "task with instructions",
			step: stepWithConfig(t, OnboardingStepKindTask, OnboardingStepConfig{Instructions: "Pegue a primeira issue"}),
		},
		{
			name: "task with only a url",
			step: stepWithConfig(t, OnboardingStepKindTask, OnboardingStepConfig{TaskURL: "https://board.example/1"}),
		},
		{
			// A task step with neither tells the person nothing at all.
			name:    "task with neither",
			step:    stepWithConfig(t, OnboardingStepKindTask, OnboardingStepConfig{}),
			wantErr: true,
		},
		{
			name: "verified first change request",
			step: stepWithConfig(t, OnboardingStepKindVerified, OnboardingStepConfig{
				Check: OnboardingCheckFirstChangeRequest, RepositoryID: "repo-1",
			}),
		},
		{
			name: "verified first change request without a repository",
			step: stepWithConfig(t, OnboardingStepKindVerified, OnboardingStepConfig{
				Check: OnboardingCheckFirstChangeRequest,
			}),
			wantErr: true,
		},
		{
			// Team membership without a team means "any team", which is a valid
			// thing to ask.
			name: "verified team membership without a team",
			step: stepWithConfig(t, OnboardingStepKindVerified, OnboardingStepConfig{Check: OnboardingCheckTeamMembership}),
		},
		{
			name:    "verified with an unknown check",
			step:    stepWithConfig(t, OnboardingStepKindVerified, OnboardingStepConfig{Check: "reads_minds"}),
			wantErr: true,
		},
		{
			// Both accept an empty selection, meaning "everything in the org".
			name: "architecture with no selection",
			step: stepWithConfig(t, OnboardingStepKindArchitecture, OnboardingStepConfig{}),
		},
		{
			name: "glossary with no selection",
			step: stepWithConfig(t, OnboardingStepKindGlossary, OnboardingStepConfig{}),
		},
		{
			name:    "unknown kind",
			step:    &OnboardingStep{Kind: "interpretive_dance", Title: "Passo"},
			wantErr: true,
		},
		{
			name:    "no title",
			step:    &OnboardingStep{Kind: OnboardingStepKindMarkdown, Title: "  ", Body: "conteúdo"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.step.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected a validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}
		})
	}
}

func TestOnboardingStep_ValidateRejectsNegativeEstimate(t *testing.T) {
	negative := -5
	step := &OnboardingStep{Kind: OnboardingStepKindMarkdown, Title: "Passo", Body: "x", EstimatedMinutes: &negative}
	if err := step.Validate(); err == nil {
		t.Fatal("expected an error for a negative estimate")
	}

	zero := 0
	step.EstimatedMinutes = &zero
	if err := step.Validate(); err != nil {
		t.Fatalf("zero minutes should be allowed: %v", err)
	}
}

func TestOnboardingStep_DecodeConfig_EmptyBlob(t *testing.T) {
	// A step stored before a config field existed must decode to the zero value
	// rather than erroring, or a schema addition would break existing flows.
	for _, raw := range []string{"", "{}", "null"} {
		step := &OnboardingStep{Kind: OnboardingStepKindMarkdown, Title: "Passo", Config: []byte(raw)}
		cfg, err := step.DecodeConfig()
		if err != nil {
			t.Fatalf("config %q: unexpected error: %v", raw, err)
		}
		if cfg.RepositoryID != "" || len(cfg.Items) != 0 {
			t.Fatalf("config %q decoded to %+v, want the zero value", raw, cfg)
		}
	}
}

func TestOnboardingStep_DecodeConfig_Malformed(t *testing.T) {
	step := &OnboardingStep{Kind: OnboardingStepKindMarkdown, Title: "Passo", Config: []byte(`{"items": `)}
	if _, err := step.DecodeConfig(); err == nil {
		t.Fatal("expected an error for malformed config")
	}
}

func TestOnboardingStep_References(t *testing.T) {
	step := stepWithConfig(t, OnboardingStepKindContacts, OnboardingStepConfig{
		RepositoryID:    "repo-1",
		RepositoryIDs:   []string{"repo-2", "repo-3"},
		TeamID:          "team-1",
		DocGenerationID: "doc-1",
		TermIDs:         []string{"term-1"},
		People:          []OnboardingStepContact{{UserID: "user-1"}, {UserID: "user-2"}, {Area: "sem usuário"}},
	})

	refs, err := step.References()
	if err != nil {
		t.Fatalf("References: %v", err)
	}
	// Every reference has to surface here: this is the single list the service
	// checks against the caller's organization, so anything missed would be an
	// unchecked cross-organization reference.
	if len(refs.RepositoryIDs) != 3 {
		t.Errorf("RepositoryIDs = %v, want the single and the list merged", refs.RepositoryIDs)
	}
	if len(refs.TeamIDs) != 1 || len(refs.DocGenerationIDs) != 1 || len(refs.GlossaryTermIDs) != 1 {
		t.Errorf("refs = %+v, want one of each", refs)
	}
	if len(refs.UserIDs) != 2 {
		t.Errorf("UserIDs = %v, want the two entries that name a user", refs.UserIDs)
	}
}

func TestOnboardingStepKind_CompletionMode(t *testing.T) {
	tests := []struct {
		kind     OnboardingStepKind
		required bool
		want     OnboardingCompletionMode
	}{
		{kind: OnboardingStepKindVerified, required: true, want: OnboardingCompletionVerified},
		{kind: OnboardingStepKindVerified, required: false, want: OnboardingCompletionVerified},
		// The platform cannot see any of these happen, whatever the flag says.
		{kind: OnboardingStepKindLink, required: true, want: OnboardingCompletionSelfReported},
		{kind: OnboardingStepKindChecklist, required: true, want: OnboardingCompletionSelfReported},
		{kind: OnboardingStepKindTask, required: true, want: OnboardingCompletionSelfReported},
		{kind: OnboardingStepKindMarkdown, required: true, want: OnboardingCompletionAcknowledge},
		{kind: OnboardingStepKindMarkdown, required: false, want: OnboardingCompletionAuto},
		{kind: OnboardingStepKindRepository, required: false, want: OnboardingCompletionAuto},
	}

	for _, tt := range tests {
		if got := tt.kind.CompletionMode(tt.required); got != tt.want {
			t.Errorf("%s (required=%v) = %q, want %q", tt.kind, tt.required, got, tt.want)
		}
	}
}

func TestOnboardingStepKind_OnlyVerifiedClaimsVerification(t *testing.T) {
	// The honesty invariant: no kind the platform cannot check may report itself
	// as verified, whether or not it is required.
	for _, kind := range []OnboardingStepKind{
		OnboardingStepKindMarkdown, OnboardingStepKindRepository, OnboardingStepKindTeam,
		OnboardingStepKindDoc, OnboardingStepKindArchitecture, OnboardingStepKindGlossary,
		OnboardingStepKindContacts, OnboardingStepKindChecklist, OnboardingStepKindLink,
		OnboardingStepKindTask,
	} {
		for _, required := range []bool{true, false} {
			if got := kind.CompletionMode(required); got == OnboardingCompletionVerified {
				t.Errorf("%s (required=%v) claims verification the platform never performs", kind, required)
			}
		}
	}
}

func TestOnboardingAssignment_Lifecycle(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	assignment := &OnboardingAssignment{Status: OnboardingAssignmentPending}

	if !assignment.IsLive() {
		t.Fatal("a pending assignment should be live")
	}

	assignment.MarkStarted(now)
	if assignment.Status != OnboardingAssignmentInProgress {
		t.Fatalf("status = %q, want in_progress", assignment.Status)
	}
	if assignment.StartedAt == nil || !assignment.StartedAt.Equal(now) {
		t.Fatalf("StartedAt = %v, want %v", assignment.StartedAt, now)
	}

	// Starting again must not move the clock: "time to complete" depends on the
	// first touch, not the latest one.
	later := now.Add(2 * time.Hour)
	assignment.MarkStarted(later)
	if !assignment.StartedAt.Equal(now) {
		t.Fatalf("StartedAt = %v, want the original %v", assignment.StartedAt, now)
	}

	assignment.MarkCompleted(later)
	if assignment.Status != OnboardingAssignmentCompleted || assignment.CompletedAt == nil {
		t.Fatalf("assignment = %+v, want completed with a timestamp", assignment)
	}
	if !assignment.IsLive() {
		t.Error("a completed assignment is still live — only abandoned ones are not")
	}

	assignment.Status = OnboardingAssignmentAbandoned
	if assignment.IsLive() {
		t.Error("an abandoned assignment must not be live: the unique index uses the same rule")
	}
}

func TestOnboardingAssignment_MarkCompletedWithoutStarting(t *testing.T) {
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	assignment := &OnboardingAssignment{Status: OnboardingAssignmentPending}

	// Someone can walk a one-step flow and finish before anything marked them
	// started; the row still needs a coherent StartedAt.
	assignment.MarkCompleted(now)
	if assignment.StartedAt == nil {
		t.Fatal("StartedAt is nil on a completed assignment")
	}
}

func TestOnboardingStepStatus_IsKnown(t *testing.T) {
	if !OnboardingStepDone.IsKnown() || !OnboardingStepSkipped.IsKnown() {
		t.Error("done and skipped must be known")
	}
	// "pending" is the absence of a row, never a stored value.
	if OnboardingStepStatus("pending").IsKnown() {
		t.Error("pending must not be a storable status")
	}
}

func TestGlossaryTerm_Validate(t *testing.T) {
	tests := []struct {
		name    string
		term    GlossaryTerm
		wantErr bool
	}{
		{name: "complete", term: GlossaryTerm{Term: "SLO", Definition: "Service Level Objective"}},
		{name: "no term", term: GlossaryTerm{Definition: "algo"}, wantErr: true},
		{name: "no definition", term: GlossaryTerm{Term: "SLO"}, wantErr: true},
		{name: "blank definition", term: GlossaryTerm{Term: "SLO", Definition: "   "}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.term.Validate()
			if tt.wantErr && err == nil {
				t.Fatal("expected an error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
