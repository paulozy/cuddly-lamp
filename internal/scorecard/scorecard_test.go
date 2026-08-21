package scorecard

import "testing"

// healthy is a repository that passes everything, so each test can express
// exactly one deviation and nothing else.
func healthy() RepoFacts {
	return RepoFacts{
		HasOwnerTeam:   true,
		Description:    "Serviço de pagamentos",
		SyncStatus:     "synced",
		HasEverSynced:  true,
		HasCoverage:    true,
		CoverageStatus: "ok",
		HasDocs:        true,
		HasWebhook:     true,
	}
}

func verdictFor(t *testing.T, s Summary, id string) Verdict {
	t.Helper()
	for _, v := range s.Verdicts {
		if v.CheckID == id {
			return v
		}
	}
	t.Fatalf("no verdict for %q", id)
	return Verdict{}
}

func TestEvaluate_HealthyRepositoryPassesEverything(t *testing.T) {
	s := Evaluate(healthy())

	if s.Failing != 0 {
		t.Errorf("failing = %d, want 0", s.Failing)
	}
	if s.NotApplicable != 0 {
		t.Errorf("not applicable = %d, want 0", s.NotApplicable)
	}
	if s.Passing != s.Total || s.Total != len(CheckIDs()) {
		t.Errorf("passing=%d total=%d, want both = %d", s.Passing, s.Total, len(CheckIDs()))
	}
}

func TestEvaluate_EachCheckFailsOnItsOwnSignal(t *testing.T) {
	tests := []struct {
		checkID string
		mutate  func(*RepoFacts)
	}{
		{"ownership.has_owner_team", func(f *RepoFacts) { f.HasOwnerTeam = false }},
		{"catalog.has_description", func(f *RepoFacts) { f.Description = "   " }},
		{"sync.healthy", func(f *RepoFacts) { f.SyncStatus = "error" }},
		{"docs.has_generated_docs", func(f *RepoFacts) { f.HasDocs = false }},
		{"delivery.webhook_registered", func(f *RepoFacts) { f.HasWebhook = false }},
		{"quality.coverage_reported", func(f *RepoFacts) { f.HasCoverage = false }},
	}

	for _, tt := range tests {
		t.Run(tt.checkID, func(t *testing.T) {
			facts := healthy()
			tt.mutate(&facts)
			s := Evaluate(facts)

			if got := verdictFor(t, s, tt.checkID).Status; got != StatusFail {
				t.Errorf("status = %q, want fail", got)
			}
			// One broken signal must not knock over unrelated checks.
			if s.Failing != 1 {
				t.Errorf("failing = %d, want exactly 1", s.Failing)
			}
		})
	}
}

// "We never measured this" is not the same as "this repository failed".
// Rendering the former as red is how a scorecard stops being believed.
func TestEvaluate_UnmeasuredSignalsAreNotApplicableRatherThanFailures(t *testing.T) {
	t.Run("never synced", func(t *testing.T) {
		facts := healthy()
		facts.HasEverSynced = false
		facts.SyncStatus = "idle"
		s := Evaluate(facts)

		if got := verdictFor(t, s, "sync.healthy").Status; got != StatusNotApplicable {
			t.Errorf("status = %q, want not_applicable", got)
		}
		if s.Failing != 0 {
			t.Errorf("failing = %d, want 0", s.Failing)
		}
	})

	t.Run("webhooks unavailable in this deployment", func(t *testing.T) {
		facts := healthy()
		facts.HasWebhook = false
		facts.WebhookRegistrationSkipped = true
		s := Evaluate(facts)

		if got := verdictFor(t, s, "delivery.webhook_registered").Status; got != StatusNotApplicable {
			t.Errorf("status = %q, want not_applicable", got)
		}
		if s.Failing != 0 {
			t.Errorf("failing = %d, want 0 — a platform limitation is not the repo's fault", s.Failing)
		}
	})
}

// Total counts judged checks only, so a not_applicable never drags the count
// down as though it were a failure.
func TestEvaluate_NotApplicableIsExcludedFromTotal(t *testing.T) {
	facts := healthy()
	facts.HasEverSynced = false

	s := Evaluate(facts)

	if s.Total != len(CheckIDs())-1 {
		t.Errorf("total = %d, want %d", s.Total, len(CheckIDs())-1)
	}
	if s.Passing != s.Total {
		t.Errorf("passing = %d, want %d", s.Passing, s.Total)
	}
}

// A coverage report that exists but could not be parsed is a real failure, not
// an absence — the CI thinks it is reporting and it is not.
func TestEvaluate_UnparseableCoverageFails(t *testing.T) {
	facts := healthy()
	facts.CoverageStatus = "failed"

	if got := verdictFor(t, Evaluate(facts), "quality.coverage_reported").Status; got != StatusFail {
		t.Errorf("status = %q, want fail", got)
	}
}

// A brand-new repository should be mostly red — that is the honest reading, and
// it is the reason the aggregate is a count rather than a percentage.
func TestEvaluate_FreshRepositoryIsActionableNotEmpty(t *testing.T) {
	s := Evaluate(RepoFacts{SyncStatus: "idle"})

	if s.Passing != 0 {
		t.Errorf("passing = %d, want 0 for a fresh repository", s.Passing)
	}
	for _, v := range s.Verdicts {
		if v.Reason == "" {
			t.Errorf("check %q failed without telling the user what to do", v.CheckID)
		}
	}
}

// IDs are persisted history keys once results are stored; renaming one silently
// orphans its history.
func TestCheckIDs_AreStableAndUnique(t *testing.T) {
	want := []string{
		"ownership.has_owner_team",
		"catalog.has_description",
		"sync.healthy",
		"docs.has_generated_docs",
		"delivery.webhook_registered",
		"quality.coverage_reported",
	}
	got := CheckIDs()

	if len(got) != len(want) {
		t.Fatalf("got %d checks, want %d", len(got), len(want))
	}
	seen := map[string]bool{}
	for i, id := range got {
		if id != want[i] {
			t.Errorf("check %d = %q, want %q", i, id, want[i])
		}
		if seen[id] {
			t.Errorf("duplicate check id %q", id)
		}
		seen[id] = true
	}
}
