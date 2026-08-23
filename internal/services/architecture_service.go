package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/detect"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs"
	"github.com/paulozy/idp-with-ai-backend/internal/jobs/tasks"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
)

// ArchitectureService runs the two passes of architecture derivation.
//
// Pass one is extraction: per repository, does network I/O, records facts. It
// runs inside the sync that already read the tree, so it costs no extra listing.
// Pass two is reconciliation: per organization, pure, reads only facts. That
// split is what makes re-deriving cheap and deterministic — and it is forced by
// the domain, because an internal edge is inherently org-wide.
//
// Everything here is best-effort by the same policy the sync already follows: a
// repository we cannot inspect reports "unknown", never a confident "has none",
// and never turns a healthy sync into sync_status=error — which would fail the
// scorecard's sync.healthy check as collateral damage.
type ArchitectureService struct {
	repo     storage.Repository
	enqueuer jobs.Enqueuer

	// extractors run in pass one. Registered rather than hard-coded so a new
	// ecosystem or sniff is one entry, and so tests can run one in isolation.
	extractors []Extractor
	// edgeDerivers run in pass two.
	edgeDerivers []EdgeDeriver

	// now is injectable so the mark-and-sweep window is assertable.
	now func() time.Time
}

// derivationDebounce is how long an enqueued reconciliation waits before it
// runs.
//
// Syncing 50 repositories would otherwise queue 50 reconciliations of the same
// organization, 49 of them redundant. The delay plus a per-organization task ID
// lets the broker collapse them: the first enqueue wins, the rest conflict and
// are dropped, and the one that runs sees every fact the batch wrote.
const derivationDebounce = 30 * time.Second

// FileFetcher reads one file from a repository. It is the narrow half of
// scm.FileReader an extractor gets, with the ref already bound.
type FileFetcher func(ctx context.Context, path string) ([]byte, error)

// ExtractInput is everything an extractor is allowed to see.
//
// Notably it holds the tree, not the provider: an extractor cannot list, cannot
// clone, and cannot reach anything the caller did not put here. Truncated is
// carried explicitly because a path missing from a truncated listing proves
// nothing, and every extractor has to say so in its Outcome.
type ExtractInput struct {
	RepositoryID string
	// Paths is the blob listing, vendored entries included — filtering is each
	// rule's business, because detect.IsVendored is the single shared list.
	Paths []string
	// Sizes maps path to the provider's reported byte count, 0 when the provider
	// does not report one (GitLab's tree listing does not).
	Sizes     map[string]int
	Truncated bool
	// Fetch reads a file. scm.ErrNotFound means absent and leaves the fact
	// complete; anything else means the fact is incomplete.
	Fetch FileFetcher
}

// Readable reports whether path is worth fetching.
//
// A 300 KB package.json is generated, not written, and is the source of truth
// for nothing; skipping it is what keeps a committed bundle out of the index. A
// provider that reports no size (GitLab's tree listing does not) leaves this
// permissive, and scm.MaxFileBytes truncates the read instead.
func (in ExtractInput) Readable(path string) bool {
	size, reported := in.Sizes[path]
	return !reported || size <= scm.MaxFileBytes
}

// Shortlist returns the paths an extractor should fetch: those matching, minus
// vendored trees, minus oversized entries.
//
// Vendoring is filtered through detect.IsVendored rather than a local copy of
// the list, because one auditable list is what kills the whole false-positive
// class — `node_modules/lodash/package.json` is not a manifest of this
// repository — and two copies of it diverge within a year.
func (in ExtractInput) Shortlist(match func(path string) bool) []string {
	out := make([]string, 0, 8)
	for _, path := range in.Paths {
		if detect.IsVendored(path) || !in.Readable(path) || !match(path) {
			continue
		}
		out = append(out, path)
	}
	return out
}

// Extractor turns one repository's files into one kind of fact.
type Extractor interface {
	// Kind is the fact_kind row this extractor owns.
	Kind() models.RepositoryFactKind
	// Version is bumped when the extraction logic changes in a way that makes
	// stored payloads stale. A bump forces a re-extraction even when the tree
	// SHA is unchanged.
	Version() int
	// Extract is I/O-bound but decides nothing destructive: it returns a payload
	// and an Outcome whose Complete field is the only thing that can ever
	// authorise a delete downstream.
	Extract(ctx context.Context, in ExtractInput) (any, derive.Outcome)
}

// DerivedEdge is one edge a deriver asserts should exist.
type DerivedEdge struct {
	SourceRepositoryID string
	TargetRepositoryID string
	Kind               models.RepositoryRelationshipKind
	Source             models.RepositoryRelationshipSource
	Confidence         float64
	Label              string
	// Fingerprint identifies the *fact* behind the edge, not the edge's
	// mutable attributes. Anything that legitimately changes without the edge
	// changing — a version bump, a renamed title — must stay out of it, or every
	// such change sweeps the row and recreates it under a new id.
	Fingerprint string
	Metadata    map[string]any
}

// DerivedSet is a deriver's complete answer for one organization.
type DerivedSet struct {
	Edges []DerivedEdge
	// Complete is false when any fact the deriver read was incomplete. It gates
	// the sweep, and nothing else does: without it a rate-limited extraction
	// looks exactly like a dependency that was deleted.
	Complete bool
	// Reasons names why, for the log and eventually the UI.
	Reasons []string
}

// EdgeDeriver reconciles one family of edges for an organization.
type EdgeDeriver interface {
	// Key is the derivation key every row this deriver writes carries, and the
	// scope of its sweep. The version segment is what allows re-keying on
	// purpose: bump it and the previous key's rows are retired on the next run.
	Key(organizationID string) string
	// Derive is a pure function over stored facts. It performs no provider I/O,
	// which is what makes re-deriving cheap and the result reproducible.
	Derive(ctx context.Context, organizationID string, facts []models.RepositoryFact) (DerivedSet, error)
	// FactKind is the fact this deriver reads.
	FactKind() models.RepositoryFactKind
}

func NewArchitectureService(repo storage.Repository, enqueuer jobs.Enqueuer) *ArchitectureService {
	return &ArchitectureService{
		repo:       repo,
		enqueuer:   enqueuer,
		extractors: defaultExtractors(),
		// Left empty until phase 1 registers the library-dependency deriver.
		edgeDerivers: defaultEdgeDerivers(),
		now:          func() time.Time { return time.Now().UTC() },
	}
}

// ── pass one: extraction ─────────────────────────────────────────────────────

// ExtractFacts records every fact kind for one repository and reports whether
// anything changed.
//
// It never returns an error. Extraction is a bonus on top of a sync, and a
// failure here must not fail the sync — see the type comment. Callers use the
// bool only to decide whether a reconciliation is worth queueing.
func (s *ArchitectureService) ExtractFacts(ctx context.Context, repo *models.Repository, reader scm.FileReader, ref scm.RepoRef, gitRef string, tree *scm.RepoTree) bool {
	if repo == nil || repo.OrganizationID == "" || tree == nil || reader == nil {
		return false
	}

	paths := tree.BlobPaths()
	sizes := make(map[string]int, len(tree.Entries))
	for i := range tree.Entries {
		if tree.Entries[i].Type == scm.TreeEntryBlob {
			sizes[tree.Entries[i].Path] = tree.Entries[i].Size
		}
	}

	changed := false
	for _, extractor := range s.extractors {
		if s.extractOne(ctx, repo, reader, ref, gitRef, tree, paths, sizes, extractor) {
			changed = true
		}
	}
	return changed
}

func (s *ArchitectureService) extractOne(ctx context.Context, repo *models.Repository, reader scm.FileReader, ref scm.RepoRef, gitRef string, tree *scm.RepoTree, paths []string, sizes map[string]int, extractor Extractor) bool {
	kind := extractor.Kind()

	existing, err := s.repo.GetRepositoryFact(ctx, repo.ID, kind)
	if err != nil {
		utils.Warn("architecture: could not read stored fact", "repo_id", repo.ID, "fact_kind", kind, "error", err)
		return false
	}
	// Same tree, same files, same extractor: the facts cannot have changed. The
	// version check is what makes a logic change re-extract anyway.
	//
	// An incomplete stored fact is deliberately retried even at the same SHA:
	// the reason it was incomplete was usually transient, and leaving it stuck
	// would mean the sweep can never run for this organization again.
	if existing != nil && existing.Complete &&
		existing.TreeSHA != "" && tree.SHA != "" &&
		existing.TreeSHA == tree.SHA &&
		existing.ExtractorVersion == extractor.Version() {
		return false
	}

	in := ExtractInput{
		RepositoryID: repo.ID,
		Paths:        paths,
		Sizes:        sizes,
		Truncated:    tree.Truncated,
		Fetch: func(ctx context.Context, path string) ([]byte, error) {
			return reader.GetFileContent(ctx, ref, gitRef, path)
		},
	}

	payload, outcome := extractor.Extract(ctx, in)
	encoded, err := json.Marshal(map[string]any{"data": payload, "outcome": outcome})
	if err != nil {
		utils.Warn("architecture: could not encode fact payload", "repo_id", repo.ID, "fact_kind", kind, "error", err)
		return false
	}

	fact := &models.RepositoryFact{
		OrganizationID:   repo.OrganizationID,
		RepositoryID:     repo.ID,
		FactKind:         kind,
		Payload:          encoded,
		TreeSHA:          tree.SHA,
		Complete:         outcome.Complete,
		ExtractorVersion: extractor.Version(),
		ExtractedAt:      s.now(),
	}
	if err := s.repo.UpsertRepositoryFact(ctx, fact); err != nil {
		utils.Warn("architecture: could not store fact", "repo_id", repo.ID, "fact_kind", kind, "error", err)
		return false
	}
	if !outcome.Complete {
		utils.Info("architecture: fact stored incomplete, sweep will be skipped",
			"repo_id", repo.ID, "fact_kind", kind, "reasons", strings.Join(outcome.Reasons, ","))
	}
	return true
}

// EnqueueDerivation queues one reconciliation for an organization, debounced.
//
// The task ID is the organization, so a batch of syncs collapses into a single
// run at the broker: the first enqueue wins and the rest come back as
// ErrTaskIDConflict, which is the expected outcome and not an error worth
// surfacing.
func (s *ArchitectureService) EnqueueDerivation(ctx context.Context, organizationID string) {
	if s.enqueuer == nil || organizationID == "" {
		return
	}
	payload := tasks.DeriveArchitecturePayload{OrganizationID: organizationID}
	err := s.enqueuer.EnqueueIn(ctx, tasks.TypeDeriveArchitecture, payload, derivationDebounce,
		asynq.TaskID(DerivationTaskID(organizationID)))
	switch {
	case err == nil:
	case errors.Is(err, asynq.ErrTaskIDConflict):
		// A reconciliation for this organization is already queued and will see
		// the facts this sync just wrote. Nothing to do.
	default:
		utils.Warn("architecture: could not enqueue derivation", "organization_id", organizationID, "error", err)
	}
}

// DerivationTaskID is the broker-level dedup key for one organization's
// reconciliation.
func DerivationTaskID(organizationID string) string {
	return "architecture-derive:" + organizationID
}

// ── pass two: reconciliation ─────────────────────────────────────────────────

// ReconcileStats is what one derivation run did, for the log.
type ReconcileStats struct {
	Upserted int
	Swept    int64
	// SweepSkipped is true when the facts were incomplete, so retraction was
	// not authorised. It is not a failure — it is the guarantee working.
	SweepSkipped bool
}

// Reconcile derives everything for one organization: the per-repository nodes
// first, then the org-wide edges.
//
// The order matters for the edges that point at those nodes — a `provides` edge
// needs the API row to exist — and the two scopes stay separate because their
// completeness gates are different. One repository's truncated tree withdraws
// only *its own* API sweep, while the same truncation withdraws the whole
// organization's library sweep, because the package index is org-wide.
func (s *ArchitectureService) Reconcile(ctx context.Context, organizationID string) error {
	if organizationID == "" {
		return errors.New("architecture: reconcile requires an organization id")
	}
	runStartedAt := s.now()

	if err := s.reconcileNodes(ctx, organizationID, runStartedAt); err != nil {
		return err
	}

	for _, deriver := range s.edgeDerivers {
		facts, err := s.repo.ListRepositoryFacts(ctx, organizationID, deriver.FactKind())
		if err != nil {
			return fmt.Errorf("list %s facts: %w", deriver.FactKind(), err)
		}
		set, err := deriver.Derive(ctx, organizationID, facts)
		if err != nil {
			return fmt.Errorf("derive %s: %w", deriver.FactKind(), err)
		}
		stats, err := s.applyEdges(ctx, organizationID, deriver.Key(organizationID), set, runStartedAt)
		if err != nil {
			return err
		}
		utils.Info("architecture: reconciled",
			"organization_id", organizationID,
			"derivation_key", deriver.Key(organizationID),
			"upserted", stats.Upserted,
			"swept", stats.Swept,
			"sweep_skipped", stats.SweepSkipped)
	}
	return nil
}

// reconcileNodes reconciles the per-repository entities: APIs, and the resources
// of phase 3.
//
// A failure on one repository does not abort the rest. The scope is per
// repository by design, so one repository the provider would not serve must not
// cost every other repository its reconciliation — the alternative is that a
// single bad tenant freezes the whole organization's graph.
func (s *ArchitectureService) reconcileNodes(ctx context.Context, organizationID string, runStartedAt time.Time) error {
	apiFacts, err := s.repo.ListRepositoryFacts(ctx, organizationID, models.FactKindAPIs)
	if err != nil {
		return fmt.Errorf("list api facts: %w", err)
	}
	for i := range apiFacts {
		stats, err := s.reconcileAPIs(ctx, organizationID, apiFacts[i], runStartedAt)
		if err != nil {
			utils.Warn("architecture: api reconciliation failed",
				"repo_id", apiFacts[i].RepositoryID, "error", err)
			continue
		}
		utils.Info("architecture: reconciled apis",
			"repo_id", apiFacts[i].RepositoryID,
			"upserted", stats.Upserted, "swept", stats.Swept, "sweep_skipped", stats.SweepSkipped)
	}

	resourceFacts, err := s.repo.ListRepositoryFacts(ctx, organizationID, models.FactKindResources)
	if err != nil {
		return fmt.Errorf("list resource facts: %w", err)
	}
	anyResourceSwept := false
	for i := range resourceFacts {
		stats, err := s.reconcileResources(ctx, organizationID, resourceFacts[i], runStartedAt)
		if err != nil {
			utils.Warn("architecture: resource reconciliation failed",
				"repo_id", resourceFacts[i].RepositoryID, "error", err)
			continue
		}
		if !stats.SweepSkipped {
			anyResourceSwept = true
		}
		utils.Info("architecture: reconciled resources",
			"repo_id", resourceFacts[i].RepositoryID,
			"upserted", stats.Upserted, "swept", stats.Swept, "sweep_skipped", stats.SweepSkipped)
	}
	// A shared resource is orphaned only once the *last* repository stops naming
	// it, which no single repository's sweep can determine. So this runs after all
	// of them — and only if at least one was actually authorised to retract,
	// otherwise a run where every fact was incomplete would retire live rows.
	if anyResourceSwept {
		if retired, err := s.repo.RetireOrphanResources(ctx, organizationID); err != nil {
			utils.Warn("architecture: retiring orphan resources failed",
				"organization_id", organizationID, "error", err)
		} else if retired > 0 {
			utils.Info("architecture: retired orphan resources",
				"organization_id", organizationID, "count", retired)
		}
	}
	return nil
}

// applyEdges writes a deriver's answer and retracts what disappeared.
//
// Two invariants hold here and are each covered by a named test:
//
//   - Nothing derived ever touches a human row. Every write carries the
//     derivation key and the sweep is scoped by it, so a row with a NULL key
//     cannot be matched by either.
//   - The sweep runs only when the facts were complete. A truncated tree, a 429
//     or a 5xx are indistinguishable from "the dependency was removed", and
//     acting on that ambiguity deletes correct edges.
func (s *ArchitectureService) applyEdges(ctx context.Context, organizationID, derivationKey string, set DerivedSet, runStartedAt time.Time) (ReconcileStats, error) {
	stats := ReconcileStats{}

	suppressed, err := s.suppressedFingerprints(ctx, organizationID, derivationKey)
	if err != nil {
		return stats, err
	}

	// last_seen_at is stamped strictly after runStartedAt so the sweep's
	// `last_seen_at < runStartedAt` can never catch a row this run just wrote.
	seenAt := runStartedAt.Add(time.Millisecond)

	for _, edge := range set.Edges {
		if edge.SourceRepositoryID == "" || edge.TargetRepositoryID == "" ||
			edge.SourceRepositoryID == edge.TargetRepositoryID {
			// A self-edge is filtered here rather than left to the database's
			// no_self_repository_relationship CHECK, so a monorepo that depends
			// on a package it publishes does not turn into a write error.
			continue
		}
		if suppressed[edge.Fingerprint] {
			continue
		}

		key := derivationKey
		fingerprint := edge.Fingerprint
		rel := &models.RepositoryRelationship{
			OrganizationID:        organizationID,
			SourceRepositoryID:    edge.SourceRepositoryID,
			TargetRepositoryID:    edge.TargetRepositoryID,
			Kind:                  edge.Kind,
			Label:                 edge.Label,
			Source:                edge.Source,
			Confidence:            edge.Confidence,
			Metadata:              edge.Metadata,
			DerivationKey:         &key,
			DerivationFingerprint: &fingerprint,
			LastSeenAt:            &seenAt,
		}
		if err := s.repo.UpsertDerivedRelationship(ctx, rel); err != nil {
			return stats, fmt.Errorf("upsert derived relationship: %w", err)
		}
		stats.Upserted++
	}

	if !set.Complete {
		stats.SweepSkipped = true
		utils.Info("architecture: skipping sweep, facts incomplete",
			"organization_id", organizationID,
			"derivation_key", derivationKey,
			"reasons", strings.Join(set.Reasons, ","))
		return stats, nil
	}

	swept, err := s.repo.SweepDerivedRelationships(ctx, derivationKey, runStartedAt)
	if err != nil {
		return stats, fmt.Errorf("sweep derived relationships: %w", err)
	}
	stats.Swept = swept
	return stats, nil
}

func (s *ArchitectureService) suppressedFingerprints(ctx context.Context, organizationID, derivationKey string) (map[string]bool, error) {
	suppressions, err := s.repo.ListSuppressions(ctx, organizationID, derivationKey)
	if err != nil {
		return nil, fmt.Errorf("list derivation suppressions: %w", err)
	}
	out := make(map[string]bool, len(suppressions))
	for i := range suppressions {
		out[suppressions[i].DerivationFingerprint] = true
	}
	return out, nil
}

// Fingerprint hashes the identity of a fact into a stable, short string.
//
// Callers pass the parts that identify the *fact* and nothing else. A declared
// version, a spec title or any other mutable attribute belongs in metadata: put
// it here and every change to it retires the row and recreates it under a new
// id, which breaks deep links and looks like churn in the graph.
func Fingerprint(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:16])
}

// UnmarshalFactPayload reads the `data` half of a stored fact into out and
// reports the recorded Outcome.
//
// A payload that cannot be decoded is treated as incomplete rather than as an
// error: the reconciliation of the other repositories is still worth doing, and
// an undecodable fact must never be allowed to authorise a sweep.
func UnmarshalFactPayload(fact *models.RepositoryFact, out any) derive.Outcome {
	envelope := struct {
		Data    json.RawMessage `json:"data"`
		Outcome derive.Outcome  `json:"outcome"`
	}{}
	if err := json.Unmarshal(fact.Payload, &envelope); err != nil {
		return derive.Outcome{Reasons: []string{derive.ReasonParseFailed}}
	}
	if len(envelope.Data) > 0 && out != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return derive.Outcome{Reasons: []string{derive.ReasonParseFailed}}
		}
	}
	// A stored fact can only be as trustworthy as the row says it is.
	if !fact.Complete {
		envelope.Outcome.Complete = false
	}
	return envelope.Outcome
}
