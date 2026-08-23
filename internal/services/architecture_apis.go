package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// apisExtractor discovers API contracts in a repository's tree.
//
// Two stages, and the split is the point:
//
//  1. glob the tree the sync already read — costs no extra I/O, and yields a
//     shortlist rather than a blind probe;
//  2. read those files and let the *content* decide.
//
// Stage 1 can only ever be a filter. There is no official path convention for API
// specs: no registry, no `.well-known` for repositories, and the only thing that
// exists is the OpenAPI spec's own recommendation that the entry document be
// named `openapi.yaml`. So a file named `openapi.yaml` that turns out not to be a
// spec creates nothing, and a spec in `docs/api/orders.yaml` is found anyway.
type apisExtractor struct{}

// maxSpecCandidates bounds one repository's shortlist.
//
// A repository with 200 files matching the glob is a specification repository,
// not a service, and reading all of them would cost 200 requests to learn that.
// Above the ceiling the fact is marked incomplete with `too_many_candidates`
// rather than silently truncated — a short read that looked complete would
// authorise a sweep of every spec past the cut.
const maxSpecCandidates = 20

func (apisExtractor) Kind() models.RepositoryFactKind { return models.FactKindAPIs }

func (apisExtractor) Version() int { return 1 }

func (apisExtractor) Extract(ctx context.Context, in ExtractInput) (any, derive.Outcome) {
	outcome := derive.CompleteOutcome()
	if in.Truncated {
		outcome.MarkIncomplete(derive.ReasonTreeTruncated)
	}

	candidates := in.Shortlist(derive.IsSpecCandidate)
	fact := derive.APIsFact{CandidateCount: len(candidates)}
	if len(candidates) > maxSpecCandidates {
		outcome.MarkIncomplete(derive.ReasonTooManyCandidates)
		candidates = candidates[:maxSpecCandidates]
	}

	for _, candidate := range candidates {
		content, err := in.Fetch(ctx, candidate)
		if err != nil {
			if !errors.Is(err, scm.ErrNotFound) {
				outcome.MarkIncomplete(derive.ReasonReadFailed)
				utils.Warn("architecture: spec candidate unreadable",
					"repo_id", in.RepositoryID, "path", candidate, "error", err)
			}
			continue
		}
		// A file that fails the sniff is simply not an API. That is a decision,
		// not a failure, so the fact stays complete — the difference matters
		// because a repository full of near-miss YAML must still be able to sweep
		// a spec that was genuinely deleted.
		if spec, ok := derive.SniffSpec(candidate, content); ok {
			fact.Specs = append(fact.Specs, spec)
		}
	}
	return fact, outcome
}

// ── the reconciler ───────────────────────────────────────────────────────────

// apiDeriver reconciles discovered APIs, one repository at a time.
//
// It does not implement EdgeDeriver: APIs are nodes, not edges, and their sweep
// is scoped per repository rather than per organization. That difference is
// load-bearing — API discovery reads no cross-repository index, so one
// repository's truncated tree must not block another's retraction.
type apiDeriver struct{}

const apiDiscoveryVersion = "v1"

func (apiDeriver) Key(repositoryID string) string {
	return "apidisc:" + apiDiscoveryVersion + ":repo/" + repositoryID
}

// reconcileAPIs writes one repository's discovered specs and retracts what
// disappeared.
func (s *ArchitectureService) reconcileAPIs(ctx context.Context, organizationID string, fact models.RepositoryFact, runStartedAt time.Time) (ReconcileStats, error) {
	stats := ReconcileStats{}
	derivationKey := apiDeriver{}.Key(fact.RepositoryID)

	var payload derive.APIsFact
	outcome := UnmarshalFactPayload(&fact, &payload)

	suppressed, err := s.suppressedFingerprints(ctx, organizationID, derivationKey)
	if err != nil {
		return stats, err
	}

	seenAt := runStartedAt.Add(time.Millisecond)
	for _, spec := range payload.Specs {
		// The fingerprint is the rule plus the spec path and nothing else. Title
		// and version are display attributes that legitimately change without the
		// API changing; including either would retract and recreate the row on
		// every rename or bump, resetting its id and manufacturing a history.
		fingerprint := Fingerprint(spec.RuleID, spec.Path)
		if suppressed[fingerprint] {
			continue
		}
		key, print := derivationKey, fingerprint
		api := &models.API{
			OrganizationID:        organizationID,
			RepositoryID:          fact.RepositoryID,
			SpecPath:              spec.Path,
			Kind:                  models.APIKind(spec.Kind),
			Title:                 spec.Title,
			Version:               spec.Version,
			OperationCount:        spec.OperationCount,
			DerivationKey:         &key,
			DerivationFingerprint: &print,
			LastSeenAt:            &seenAt,
			Metadata: map[string]any{
				"rule_id":    spec.RuleID,
				"confidence": spec.Confidence,
				"spec_path":  spec.Path,
			},
		}
		if err := s.repo.UpsertDerivedAPI(ctx, api); err != nil {
			return stats, fmt.Errorf("upsert derived api: %w", err)
		}
		stats.Upserted++
	}

	if !outcome.Complete {
		stats.SweepSkipped = true
		utils.Info("architecture: skipping api sweep, fact incomplete",
			"repo_id", fact.RepositoryID, "reasons", strings.Join(outcome.Reasons, ","))
		return stats, nil
	}

	swept, err := s.repo.SweepDerivedAPIs(ctx, fact.RepositoryID, derivationKey, runStartedAt)
	if err != nil {
		return stats, fmt.Errorf("sweep derived apis: %w", err)
	}
	stats.Swept = swept
	return stats, nil
}
