// Package scorecard evaluates deterministic maturity checks over a repository.
//
// The whole design rests on one rule: a check is a pure function of a RepoFacts
// value. It receives no database handle and no context, so it *cannot* issue a
// query. That makes N checks over M repositories exactly one read by
// construction — the caller loads the facts once — and it makes every check
// unit-testable against a literal struct with no fixtures.
//
// There is deliberately no score and no level here. A percentage over a handful
// of checks asserts that "has a description" is tradeable against "has an
// accountable owner", and it reads as a grade on the team rather than a list of
// things to do. Callers render the verdicts and a passing count.
package scorecard

import "strings"

// Status is the outcome of a single check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	// StatusNotApplicable is a first-class outcome, not a soft failure. It means
	// the check could not be judged — the signal was never produced — which is a
	// different thing from the repository failing it. Rendering "we never
	// measured" as red is how a scorecard loses trust on day one.
	StatusNotApplicable Status = "not_applicable"
)

// RepoFacts is the complete input to every check. If a check needs something
// that isn't here, the fact goes here first and the loader fills it — that is
// what keeps evaluation free of queries.
type RepoFacts struct {
	HasOwnerTeam bool
	Description  string
	SyncStatus   string
	// HasEverSynced separates "sync has not run yet" from "sync ran and failed".
	HasEverSynced  bool
	HasCoverage    bool
	CoverageStatus string
	HasDocs        bool
	HasWebhook     bool
	// WebhookRegistrationSkipped is true when the deployment cannot register
	// webhooks at all (a localhost WEBHOOK_BASE_URL). Failing every repository
	// for a platform-level configuration would be noise, not a finding.
	WebhookRegistrationSkipped bool
}

// Verdict is one check's outcome for one repository.
type Verdict struct {
	CheckID string `json:"check_id"`
	// Version changes when a predicate's meaning changes (a threshold moving,
	// say) so historical results stay interpretable once they are persisted.
	Version int    `json:"version"`
	Title   string `json:"title"`
	Status  Status `json:"status"`
	// Reason says what to do about it, in the user's language. It is generated
	// next to the predicate on purpose: a message assembled elsewhere drifts
	// from the condition that produced it.
	Reason string `json:"reason"`
}

// Check pairs a stable identity with its predicate. IDs are strings and must
// never be renamed — persisted history will key on them.
type Check struct {
	ID      string
	Version int
	Title   string
	Eval    func(RepoFacts) (Status, string)
}

// registry is intentionally hardcoded. Every vendor keeps predicates in code or
// config and only the packaging (enabled, grouping, wording) in data; a generic
// expression evaluator on day one would be machinery without a user.
var registry = []Check{
	{
		ID:      "ownership.has_owner_team",
		Version: 1,
		Title:   "Tem time responsável",
		Eval: func(f RepoFacts) (Status, string) {
			if f.HasOwnerTeam {
				return StatusPass, "Um time é responsável por este repositório."
			}
			return StatusFail, "Ninguém é responsável. Defina um time em Configurações do repositório."
		},
	},
	{
		ID:      "catalog.has_description",
		Version: 1,
		Title:   "Tem descrição",
		Eval: func(f RepoFacts) (Status, string) {
			if strings.TrimSpace(f.Description) != "" {
				return StatusPass, "A descrição ajuda quem não conhece o serviço."
			}
			return StatusFail, "Sem descrição. Explique em uma frase o que este serviço faz."
		},
	},
	{
		ID:      "sync.healthy",
		Version: 1,
		Title:   "Sincronização saudável",
		Eval: func(f RepoFacts) (Status, string) {
			if !f.HasEverSynced {
				return StatusNotApplicable, "Ainda não sincronizou pela primeira vez."
			}
			if f.SyncStatus == "error" {
				return StatusFail, "A última sincronização falhou. Verifique o token da organização."
			}
			return StatusPass, "Metadados do provider estão atualizados."
		},
	},
	{
		ID:      "docs.has_generated_docs",
		Version: 1,
		Title:   "Tem documentação",
		Eval: func(f RepoFacts) (Status, string) {
			if f.HasDocs {
				return StatusPass, "Existe documentação gerada para este repositório."
			}
			return StatusFail, "Sem documentação. Gere a partir da aba Documentação."
		},
	},
	{
		ID:      "delivery.webhook_registered",
		Version: 1,
		Title:   "Webhook registrado",
		Eval: func(f RepoFacts) (Status, string) {
			if f.WebhookRegistrationSkipped {
				return StatusNotApplicable, "Esta instalação não expõe uma URL pública para webhooks."
			}
			if f.HasWebhook {
				return StatusPass, "O provider notifica mudanças automaticamente."
			}
			return StatusFail, "Sem webhook. Os metadados só atualizam quando alguém abre o repositório."
		},
	},
	{
		ID:      "quality.coverage_reported",
		Version: 1,
		Title:   "Cobertura reportada",
		Eval: func(f RepoFacts) (Status, string) {
			// Deliberately not a threshold. Agreeing on a number is a separate
			// conversation; "the CI reports coverage at all" is actionable today
			// and needs nobody's agreement.
			if !f.HasCoverage {
				return StatusFail, "O CI nunca enviou cobertura. Gere um token na aba Configurações."
			}
			if f.CoverageStatus == "failed" {
				return StatusFail, "O último relatório de cobertura não pôde ser lido."
			}
			return StatusPass, "O CI está reportando cobertura."
		},
	},
}

// Summary is the rendered result for one repository. Passing is a count, not a
// ratio: a count reads as a to-do list, a percentage reads as a grade.
type Summary struct {
	Passing       int `json:"passing"`
	Failing       int `json:"failing"`
	NotApplicable int `json:"not_applicable"`
	// Total counts only the checks that were actually judged, so a repository is
	// never penalised for a check that did not apply to it.
	Total    int       `json:"total"`
	Verdicts []Verdict `json:"verdicts"`
}

// Evaluate runs every check against one repository's facts.
func Evaluate(facts RepoFacts) Summary {
	summary := Summary{Verdicts: make([]Verdict, 0, len(registry))}
	for _, check := range registry {
		status, reason := check.Eval(facts)
		summary.Verdicts = append(summary.Verdicts, Verdict{
			CheckID: check.ID,
			Version: check.Version,
			Title:   check.Title,
			Status:  status,
			Reason:  reason,
		})
		switch status {
		case StatusPass:
			summary.Passing++
			summary.Total++
		case StatusFail:
			summary.Failing++
			summary.Total++
		case StatusNotApplicable:
			summary.NotApplicable++
		}
	}
	return summary
}

// CheckIDs exposes the registry's identities for callers that aggregate across
// repositories without re-deriving them.
func CheckIDs() []string {
	ids := make([]string, 0, len(registry))
	for _, c := range registry {
		ids = append(ids, c.ID)
	}
	return ids
}
