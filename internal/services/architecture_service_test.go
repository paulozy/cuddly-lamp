package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/paulozy/idp-with-ai-backend/internal/derive"
	"github.com/paulozy/idp-with-ai-backend/internal/integrations/scm"
	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

// ── store double ─────────────────────────────────────────────────────────────
//
// The methods below reproduce migration 033's SQL, statement for statement,
// because that SQL *is* the behaviour under test:
//
//   - identity is (org, source, target, kind, derivation_key, fingerprint)
//   - the unique index is partial on `derivation_key IS NOT NULL`, so a human
//     row is never constrained and two identical manual edges stay legal
//   - a live row wins over a soft-deleted twin, and reviving keeps the id
//   - the sweep matches `derivation_key = $1`, which NULL can never satisfy
//
// Phase 1's end-to-end scenario asserts the same guarantees against real
// Postgres. This double is here so each one has a named unit test that says
// which rule broke.

type architectureStore struct {
	storage.Repository
	facts          map[string]*models.RepositoryFact
	relationships  []*models.RepositoryRelationship
	apis           []*models.API
	resources      []*models.Resource
	links          []*models.RepositoryResource
	suppressions   []models.DerivationSuppression
	nextID         int
	sweepCalls     int
	apiSweepCalls  int
	linkSweepCalls int
	retireCalls    int
	upsertErr      error
}

func newArchitectureStore() *architectureStore {
	return &architectureStore{facts: make(map[string]*models.RepositoryFact)}
}

func factKey(repositoryID string, kind models.RepositoryFactKind) string {
	return repositoryID + "|" + string(kind)
}

func (s *architectureStore) UpsertRepositoryFact(_ context.Context, fact *models.RepositoryFact) error {
	copied := *fact
	s.facts[factKey(fact.RepositoryID, fact.FactKind)] = &copied
	return nil
}

func (s *architectureStore) GetRepositoryFact(_ context.Context, repositoryID string, kind models.RepositoryFactKind) (*models.RepositoryFact, error) {
	return s.facts[factKey(repositoryID, kind)], nil
}

func (s *architectureStore) ListRepositoryFacts(_ context.Context, organizationID string, kind models.RepositoryFactKind) ([]models.RepositoryFact, error) {
	var out []models.RepositoryFact
	for _, fact := range s.facts {
		if fact.OrganizationID == organizationID && fact.FactKind == kind {
			out = append(out, *fact)
		}
	}
	return out, nil
}

func (s *architectureStore) UpsertDerivedRelationship(_ context.Context, rel *models.RepositoryRelationship) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	var match *models.RepositoryRelationship
	for _, existing := range s.relationships {
		if existing.OrganizationID != rel.OrganizationID ||
			existing.SourceRepositoryID != rel.SourceRepositoryID ||
			existing.TargetRepositoryID != rel.TargetRepositoryID ||
			existing.Kind != rel.Kind ||
			!existing.IsDerived() ||
			*existing.DerivationKey != *rel.DerivationKey ||
			*existing.DerivationFingerprint != *rel.DerivationFingerprint {
			continue
		}
		// A live row wins over a soft-deleted twin.
		if match == nil || (match.DeletedAt != nil && existing.DeletedAt == nil) {
			match = existing
		}
	}
	if match == nil {
		s.nextID++
		rel.ID = fmt.Sprintf("rel-%d", s.nextID)
		copied := *rel
		s.relationships = append(s.relationships, &copied)
		return nil
	}
	match.Label = rel.Label
	match.Source = rel.Source
	match.Confidence = rel.Confidence
	match.Metadata = rel.Metadata
	match.LastSeenAt = rel.LastSeenAt
	match.DeletedAt = nil
	rel.ID = match.ID
	return nil
}

func (s *architectureStore) SweepDerivedRelationships(_ context.Context, derivationKey string, runStartedAt time.Time) (int64, error) {
	s.sweepCalls++
	now := time.Now().UTC()
	var swept int64
	for _, rel := range s.relationships {
		// `derivation_key = $1` — a human row carries NULL and cannot match.
		if !rel.IsDerived() || *rel.DerivationKey != derivationKey {
			continue
		}
		if rel.DeletedAt != nil || rel.LastSeenAt == nil || !rel.LastSeenAt.Before(runStartedAt) {
			continue
		}
		deleted := now
		rel.DeletedAt = &deleted
		swept++
	}
	return swept, nil
}

func (s *architectureStore) UpsertDerivedAPI(_ context.Context, api *models.API) error {
	for _, existing := range s.apis {
		if existing.RepositoryID == api.RepositoryID && existing.SpecPath == api.SpecPath {
			// The location is the identity, so this is the same API: refresh its
			// attributes and revive it, keeping the id.
			existing.Kind = api.Kind
			existing.Title = api.Title
			existing.Version = api.Version
			existing.OperationCount = api.OperationCount
			existing.Metadata = api.Metadata
			existing.LastSeenAt = api.LastSeenAt
			existing.DeletedAt = nil
			api.ID = existing.ID
			return nil
		}
	}
	s.nextID++
	api.ID = fmt.Sprintf("api-%d", s.nextID)
	copied := *api
	s.apis = append(s.apis, &copied)
	return nil
}

func (s *architectureStore) SweepDerivedAPIs(_ context.Context, repositoryID, derivationKey string, runStartedAt time.Time) (int64, error) {
	s.apiSweepCalls++
	now := time.Now().UTC()
	var swept int64
	for _, api := range s.apis {
		if api.RepositoryID != repositoryID || !api.IsDerived() || *api.DerivationKey != derivationKey {
			continue
		}
		if api.DeletedAt != nil || api.LastSeenAt == nil || !api.LastSeenAt.Before(runStartedAt) {
			continue
		}
		deleted := now
		api.DeletedAt = &deleted
		swept++
	}
	return swept, nil
}

func (s *architectureStore) ListAPIs(_ context.Context, organizationID string) ([]models.API, error) {
	var out []models.API
	for _, api := range s.apis {
		if api.OrganizationID == organizationID && api.DeletedAt == nil {
			out = append(out, *api)
		}
	}
	return out, nil
}

func (s *architectureStore) liveAPIs() []*models.API {
	var out []*models.API
	for _, api := range s.apis {
		if api.DeletedAt == nil {
			out = append(out, api)
		}
	}
	return out
}

// The resource methods reproduce migration 035's two unique indexes. The
// asymmetry between them IS the design: the shared identity is the locator, so two
// repositories naming the same host converge on one row, while the scoped identity
// includes the repository, so two local engines of the same kind stay two rows.
func (s *architectureStore) UpsertDerivedResource(_ context.Context, resource *models.Resource) error {
	for _, existing := range s.resources {
		if existing.OrganizationID != resource.OrganizationID || existing.Engine != resource.Engine {
			continue
		}
		if resource.IsScoped() != existing.IsScoped() {
			continue
		}
		match := false
		if resource.IsScoped() {
			match = *existing.ScopedRepositoryID == *resource.ScopedRepositoryID &&
				existing.DisplayName == resource.DisplayName
		} else {
			match = existing.Host == resource.Host &&
				portKey(existing.Port) == portKey(resource.Port) &&
				existing.Namespace == resource.Namespace
		}
		if !match {
			continue
		}
		existing.DisplayName = resource.DisplayName
		existing.Metadata = resource.Metadata
		existing.LastSeenAt = resource.LastSeenAt
		existing.DeletedAt = nil
		resource.ID = existing.ID
		return nil
	}
	s.nextID++
	resource.ID = fmt.Sprintf("res-%d", s.nextID)
	copied := *resource
	s.resources = append(s.resources, &copied)
	return nil
}

func portKey(port *int) string {
	if port == nil {
		return "-"
	}
	return fmt.Sprintf("%d", *port)
}

func (s *architectureStore) UpsertDerivedRepositoryResource(_ context.Context, link *models.RepositoryResource) error {
	for _, existing := range s.links {
		if existing.RepositoryID != link.RepositoryID || existing.ResourceID != link.ResourceID ||
			!existing.IsDerived() || !link.IsDerived() ||
			*existing.DerivationKey != *link.DerivationKey ||
			*existing.DerivationFingerprint != *link.DerivationFingerprint {
			continue
		}
		existing.Confidence = link.Confidence
		existing.Metadata = link.Metadata
		existing.LastSeenAt = link.LastSeenAt
		existing.DeletedAt = nil
		link.ID = existing.ID
		return nil
	}
	s.nextID++
	link.ID = fmt.Sprintf("link-%d", s.nextID)
	copied := *link
	s.links = append(s.links, &copied)
	return nil
}

func (s *architectureStore) SweepDerivedRepositoryResources(_ context.Context, repositoryID, derivationKey string, runStartedAt time.Time) (int64, error) {
	s.linkSweepCalls++
	now := time.Now().UTC()
	var swept int64
	for _, link := range s.links {
		if link.RepositoryID != repositoryID || !link.IsDerived() || *link.DerivationKey != derivationKey {
			continue
		}
		if link.DeletedAt != nil || link.LastSeenAt == nil || !link.LastSeenAt.Before(runStartedAt) {
			continue
		}
		deleted := now
		link.DeletedAt = &deleted
		swept++
	}
	return swept, nil
}

// RetireOrphanResources mirrors the NOT EXISTS in the SQL: a shared resource is
// orphaned only once the last repository stops naming it.
func (s *architectureStore) RetireOrphanResources(_ context.Context, organizationID string) (int64, error) {
	s.retireCalls++
	referenced := map[string]bool{}
	for _, link := range s.links {
		if link.DeletedAt == nil {
			referenced[link.ResourceID] = true
		}
	}
	now := time.Now().UTC()
	var retired int64
	for _, resource := range s.resources {
		if resource.OrganizationID != organizationID || resource.DeletedAt != nil || !resource.IsDerived() {
			continue
		}
		if referenced[resource.ID] {
			continue
		}
		deleted := now
		resource.DeletedAt = &deleted
		retired++
	}
	return retired, nil
}

func (s *architectureStore) ListResources(_ context.Context, organizationID string) ([]models.Resource, error) {
	var out []models.Resource
	for _, resource := range s.resources {
		if resource.OrganizationID == organizationID && resource.DeletedAt == nil {
			out = append(out, *resource)
		}
	}
	return out, nil
}

func (s *architectureStore) ListRepositoryResources(_ context.Context, organizationID string) ([]models.RepositoryResource, error) {
	var out []models.RepositoryResource
	for _, link := range s.links {
		if link.OrganizationID == organizationID && link.DeletedAt == nil {
			out = append(out, *link)
		}
	}
	return out, nil
}

func (s *architectureStore) liveResources() []*models.Resource {
	var out []*models.Resource
	for _, resource := range s.resources {
		if resource.DeletedAt == nil {
			out = append(out, resource)
		}
	}
	return out
}

func (s *architectureStore) liveLinks() []*models.RepositoryResource {
	var out []*models.RepositoryResource
	for _, link := range s.links {
		if link.DeletedAt == nil {
			out = append(out, link)
		}
	}
	return out
}

func (s *architectureStore) ListSuppressions(_ context.Context, organizationID, derivationKey string) ([]models.DerivationSuppression, error) {
	var out []models.DerivationSuppression
	for _, suppression := range s.suppressions {
		if suppression.OrganizationID == organizationID &&
			(derivationKey == "" || suppression.DerivationKey == derivationKey) {
			out = append(out, suppression)
		}
	}
	return out, nil
}

func (s *architectureStore) live() []*models.RepositoryRelationship {
	var out []*models.RepositoryRelationship
	for _, rel := range s.relationships {
		if rel.DeletedAt == nil {
			out = append(out, rel)
		}
	}
	return out
}

// ── test doubles for the two registries ──────────────────────────────────────

type stubDeriver struct {
	key  string
	kind models.RepositoryFactKind
	set  DerivedSet
	err  error
}

func (d *stubDeriver) Key(string) string                   { return d.key }
func (d *stubDeriver) FactKind() models.RepositoryFactKind { return d.kind }
func (d *stubDeriver) Derive(context.Context, string, []models.RepositoryFact) (DerivedSet, error) {
	return d.set, d.err
}

type stubExtractor struct {
	kind    models.RepositoryFactKind
	version int
	payload any
	outcome derive.Outcome
	calls   int
	// fetched records the paths the extractor actually asked the provider for.
	fetched []string
	// wants is the shortlist the extractor tries to read.
	wants func(path string) bool
}

func (e *stubExtractor) Kind() models.RepositoryFactKind { return e.kind }
func (e *stubExtractor) Version() int {
	if e.version == 0 {
		return 1
	}
	return e.version
}

func (e *stubExtractor) Extract(ctx context.Context, in ExtractInput) (any, derive.Outcome) {
	e.calls++
	if e.wants != nil {
		for _, path := range in.Shortlist(e.wants) {
			e.fetched = append(e.fetched, path)
			if _, err := in.Fetch(ctx, path); err != nil && !errors.Is(err, scm.ErrNotFound) {
				out := e.outcome
				out.MarkIncomplete(derive.ReasonReadFailed)
				return e.payload, out
			}
		}
	}
	return e.payload, e.outcome
}

// fakeEnqueuer reproduces the broker's TaskID semantics: a second task with an
// id already in flight is refused with ErrTaskIDConflict. That is what makes
// the debounce real rather than a hope, so the double has to model it.
type fakeEnqueuer struct {
	enqueued []string
	inFlight map[string]bool
}

func newFakeEnqueuer() *fakeEnqueuer {
	return &fakeEnqueuer{inFlight: make(map[string]bool)}
}

// A fake stands in for a live queue, so Available is true — the false case is
// what the no-op implementation exists to model, and a test that wants it says so
// explicitly.
func (e *fakeEnqueuer) Available() bool { return true }

func (e *fakeEnqueuer) Enqueue(_ context.Context, taskType string, _ any, opts ...asynq.Option) error {
	return e.record(taskType, opts...)
}

func (e *fakeEnqueuer) EnqueueIn(_ context.Context, taskType string, _ any, _ time.Duration, opts ...asynq.Option) error {
	return e.record(taskType, opts...)
}

func (e *fakeEnqueuer) record(taskType string, opts ...asynq.Option) error {
	for _, opt := range opts {
		if opt.Type() != asynq.TaskIDOpt {
			continue
		}
		id, _ := opt.Value().(string)
		if e.inFlight[id] {
			return asynq.ErrTaskIDConflict
		}
		e.inFlight[id] = true
	}
	e.enqueued = append(e.enqueued, taskType)
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────────────

const testOrgID = "org-1"

func newArchitectureService(store storage.Repository, enqueuer *fakeEnqueuer) *ArchitectureService {
	svc := NewArchitectureService(store, enqueuer)
	svc.extractors = nil
	svc.edgeDerivers = nil
	// A stepping clock, not a frozen one. Mark-and-sweep compares
	// `last_seen_at < runStartedAt`, so consecutive runs have to sit in
	// different windows or the sweep can never retract anything — and a frozen
	// clock would make the retraction tests pass for the wrong reason.
	step := 0
	base := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time {
		step++
		return base.Add(time.Duration(step) * time.Hour)
	}
	return svc
}

func edge(source, target, fingerprint string) DerivedEdge {
	return DerivedEdge{
		SourceRepositoryID: source,
		TargetRepositoryID: target,
		Kind:               models.RepositoryRelationshipKindLibrary,
		Source:             models.RepositoryRelationshipSourceManifest,
		Confidence:         derive.ConfidenceExactName,
		Label:              "github.com/org/" + target,
		Fingerprint:        fingerprint,
		Metadata:           map[string]any{"rule_id": "libdep.gomod"},
	}
}

func humanEdge(source, target string) *models.RepositoryRelationship {
	return &models.RepositoryRelationship{
		ID:                 "human-1",
		OrganizationID:     testOrgID,
		SourceRepositoryID: source,
		TargetRepositoryID: target,
		Kind:               models.RepositoryRelationshipKindLibrary,
		Source:             models.RepositoryRelationshipSourceManual,
		Confidence:         1.0,
	}
}

func reconcileWith(t *testing.T, store *architectureStore, svc *ArchitectureService, set DerivedSet) {
	t.Helper()
	svc.edgeDerivers = []EdgeDeriver{&stubDeriver{
		key:  "libdep:v1:org/" + testOrgID,
		kind: models.FactKindPackages,
		set:  set,
	}}
	if err := svc.Reconcile(context.Background(), testOrgID); err != nil {
		t.Fatalf("Reconcile() error = %v, want nil", err)
	}
}

// ── reconciliation ───────────────────────────────────────────────────────────

func TestReconcile_InsertsNewDerivedEdge(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())

	reconcileWith(t, store, svc, DerivedSet{Edges: []DerivedEdge{edge("repo-a", "repo-b", "fp-1")}, Complete: true})

	live := store.live()
	if len(live) != 1 {
		t.Fatalf("live relationships = %d, want 1", len(live))
	}
	got := live[0]
	if got.DerivationKey == nil || *got.DerivationKey != "libdep:v1:org/"+testOrgID {
		t.Errorf("derivation_key = %v, want libdep:v1:org/%s", got.DerivationKey, testOrgID)
	}
	if got.Source != models.RepositoryRelationshipSourceManifest {
		t.Errorf("source = %q, want %q", got.Source, models.RepositoryRelationshipSourceManifest)
	}
	if got.Confidence != 1.0 {
		t.Errorf("confidence = %v, want 1", got.Confidence)
	}
}

// Deriving twice must leave one row. Without the (key, fingerprint) identity the
// graph would gain a duplicate edge on every sync.
func TestReconcile_IsIdempotentAcrossRuns(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())
	set := DerivedSet{Edges: []DerivedEdge{edge("repo-a", "repo-b", "fp-1")}, Complete: true}

	reconcileWith(t, store, svc, set)
	firstID := store.live()[0].ID
	reconcileWith(t, store, svc, set)

	live := store.live()
	if len(live) != 1 {
		t.Fatalf("live relationships = %d, want 1", len(live))
	}
	if live[0].ID != firstID {
		t.Errorf("id = %q, want the original %q", live[0].ID, firstID)
	}
}

// The partial index only covers live rows, so a soft-deleted twin can coexist
// with an insert. Reviving instead of inserting is what keeps the id — and every
// deep link to the edge — alive.
func TestReconcile_RevivesSoftDeletedTwinKeepingID(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())
	set := DerivedSet{Edges: []DerivedEdge{edge("repo-a", "repo-b", "fp-1")}, Complete: true}

	reconcileWith(t, store, svc, set)
	original := store.live()[0].ID

	// The dependency disappears, then comes back.
	reconcileWith(t, store, svc, DerivedSet{Complete: true})
	if len(store.live()) != 0 {
		t.Fatalf("live relationships after sweep = %d, want 0", len(store.live()))
	}
	reconcileWith(t, store, svc, set)

	live := store.live()
	if len(live) != 1 {
		t.Fatalf("live relationships = %d, want 1", len(live))
	}
	if live[0].ID != original {
		t.Errorf("id = %q, want the revived %q", live[0].ID, original)
	}
}

func TestReconcile_SweepsEdgeThatDisappeared(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())

	reconcileWith(t, store, svc, DerivedSet{Edges: []DerivedEdge{edge("repo-a", "repo-b", "fp-1")}, Complete: true})
	reconcileWith(t, store, svc, DerivedSet{Complete: true})

	if got := len(store.live()); got != 0 {
		t.Errorf("live relationships = %d, want 0", got)
	}
}

// The most important test in the phase. A truncated tree, a 429 and a 5xx are
// indistinguishable from "the dependency was removed". Sweeping on that
// ambiguity deletes correct edges, so an incomplete fact must not sweep at all.
func TestReconcile_DoesNotSweepWhenFactIncomplete(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())

	reconcileWith(t, store, svc, DerivedSet{Edges: []DerivedEdge{edge("repo-a", "repo-b", "fp-1")}, Complete: true})
	sweepsAfterFirstRun := store.sweepCalls

	reconcileWith(t, store, svc, DerivedSet{
		Complete: false,
		Reasons:  []string{derive.ReasonTreeTruncated},
	})

	if store.sweepCalls != sweepsAfterFirstRun {
		t.Errorf("sweep calls = %d, want %d — an incomplete fact must not authorise a delete",
			store.sweepCalls, sweepsAfterFirstRun)
	}
	if got := len(store.live()); got != 1 {
		t.Errorf("live relationships = %d, want 1", got)
	}
}

// The structural guarantee: a human row carries a NULL derivation key, and the
// sweep's `derivation_key = $1` can never match NULL. No deriver bug can delete
// a declaration.
func TestReconcile_NeverTouchesHumanEdge(t *testing.T) {
	store := newArchitectureStore()
	store.relationships = append(store.relationships, humanEdge("repo-a", "repo-b"))
	svc := newArchitectureService(store, newFakeEnqueuer())

	// The deriver asserts nothing at all, so the sweep runs against everything.
	reconcileWith(t, store, svc, DerivedSet{Complete: true})

	live := store.live()
	if len(live) != 1 {
		t.Fatalf("live relationships = %d, want the human edge to survive", len(live))
	}
	if live[0].ID != "human-1" || live[0].IsDerived() {
		t.Errorf("surviving row = %+v, want the untouched human edge", live[0])
	}
}

// A soft delete does not survive re-derivation — the next run recomputes the
// same fingerprint and revives the row. Suppression is the only tombstone that
// holds, so the deriver has to consult it before writing.
func TestReconcile_RespectsSuppression(t *testing.T) {
	store := newArchitectureStore()
	store.suppressions = []models.DerivationSuppression{{
		OrganizationID:        testOrgID,
		DerivationKey:         "libdep:v1:org/" + testOrgID,
		DerivationFingerprint: "fp-1",
		Reason:                "vendored copy, not a real dependency",
	}}
	svc := newArchitectureService(store, newFakeEnqueuer())

	reconcileWith(t, store, svc, DerivedSet{
		Edges:    []DerivedEdge{edge("repo-a", "repo-b", "fp-1"), edge("repo-a", "repo-c", "fp-2")},
		Complete: true,
	})

	live := store.live()
	if len(live) != 1 {
		t.Fatalf("live relationships = %d, want only the unsuppressed edge", len(live))
	}
	if live[0].TargetRepositoryID != "repo-c" {
		t.Errorf("target = %q, want repo-c", live[0].TargetRepositoryID)
	}
}

// A monorepo commonly declares a dependency on a package it publishes itself.
// Filtering before the write keeps that from becoming a database error against
// the no_self_repository_relationship CHECK.
func TestReconcile_DropsSelfEdgeBeforeWriting(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())

	reconcileWith(t, store, svc, DerivedSet{
		Edges:    []DerivedEdge{edge("repo-a", "repo-a", "fp-self")},
		Complete: true,
	})

	if got := len(store.live()); got != 0 {
		t.Errorf("live relationships = %d, want 0", got)
	}
}

// ── extraction orchestration ─────────────────────────────────────────────────

func factRepo() *models.Repository {
	return &models.Repository{ID: "repo-a", OrganizationID: testOrgID}
}

type stubFileReader struct {
	contents map[string]string
	err      error
	requests []string
}

func (r *stubFileReader) GetFileContent(_ context.Context, _ scm.RepoRef, _, path string) ([]byte, error) {
	r.requests = append(r.requests, path)
	if r.err != nil {
		return nil, r.err
	}
	content, ok := r.contents[path]
	if !ok {
		return nil, scm.ErrNotFound
	}
	return []byte(content), nil
}

func tree(sha string, truncated bool, entries ...scm.TreeEntry) *scm.RepoTree {
	return &scm.RepoTree{SHA: sha, Truncated: truncated, Entries: entries}
}

func blob(path string, size int) scm.TreeEntry {
	return scm.TreeEntry{Path: path, Type: scm.TreeEntryBlob, Size: size}
}

// Same tree, same files, same extractor version: the facts cannot have changed,
// so the network must not be touched at all.
func TestExtractFacts_UnchangedTreeSHASkipsExtraction(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())
	extractor := &stubExtractor{kind: models.FactKindPackages, payload: map[string]any{"published": []string{"x"}}, outcome: derive.CompleteOutcome()}
	svc.extractors = []Extractor{extractor}

	repo := factRepo()
	reader := &stubFileReader{}
	repoTree := tree("sha-1", false, blob("go.mod", 100))

	if !svc.ExtractFacts(context.Background(), repo, reader, scm.RepoRef{}, "main", repoTree) {
		t.Fatal("first ExtractFacts() = false, want true")
	}
	if svc.ExtractFacts(context.Background(), repo, reader, scm.RepoRef{}, "main", repoTree) {
		t.Error("second ExtractFacts() = true, want false — the tree did not change")
	}
	if extractor.calls != 1 {
		t.Errorf("extractor calls = %d, want 1", extractor.calls)
	}
}

// An incomplete fact is retried even at the same SHA: whatever made it
// incomplete was usually transient, and leaving it stuck would mean this
// organization's sweep can never run again.
func TestExtractFacts_IncompleteFactIsRetriedAtSameTreeSHA(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())
	extractor := &stubExtractor{
		kind:    models.FactKindPackages,
		outcome: derive.Outcome{Complete: false, Reasons: []string{derive.ReasonReadFailed}},
	}
	svc.extractors = []Extractor{extractor}

	repo := factRepo()
	repoTree := tree("sha-1", false, blob("go.mod", 100))
	svc.ExtractFacts(context.Background(), repo, &stubFileReader{}, scm.RepoRef{}, "main", repoTree)
	svc.ExtractFacts(context.Background(), repo, &stubFileReader{}, scm.RepoRef{}, "main", repoTree)

	if extractor.calls != 2 {
		t.Errorf("extractor calls = %d, want 2", extractor.calls)
	}
}

// A 300 KB manifest is generated, not written. Skipping it before the fetch is
// what keeps a committed bundle out of the index — and out of the request budget.
func TestExtractFacts_OversizedEntryIsNeverFetched(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())
	extractor := &stubExtractor{
		kind:    models.FactKindPackages,
		outcome: derive.CompleteOutcome(),
		wants:   func(path string) bool { return path == "package.json" || path == "big/package.json" },
	}
	svc.extractors = []Extractor{extractor}

	reader := &stubFileReader{contents: map[string]string{"package.json": "{}"}}
	svc.ExtractFacts(context.Background(), factRepo(), reader, scm.RepoRef{}, "main",
		tree("sha-1", false, blob("package.json", 512), blob("big/package.json", 300*1024)))

	for _, requested := range reader.requests {
		if requested == "big/package.json" {
			t.Fatalf("requested paths = %v, want the 300 KB entry skipped", reader.requests)
		}
	}
	if len(reader.requests) != 1 {
		t.Errorf("requested paths = %v, want exactly the small manifest", reader.requests)
	}
}

// The vendored filter is detect's single list, and this is the false-positive
// class it exists for: node_modules/lodash/package.json is not a manifest of
// this repository.
func TestExtractFacts_VendoredManifestIsNeverFetched(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())
	extractor := &stubExtractor{
		kind:    models.FactKindPackages,
		outcome: derive.CompleteOutcome(),
		wants:   func(path string) bool { return len(path) >= 12 && path[len(path)-12:] == "package.json" },
	}
	svc.extractors = []Extractor{extractor}

	reader := &stubFileReader{contents: map[string]string{"package.json": "{}"}}
	svc.ExtractFacts(context.Background(), factRepo(), reader, scm.RepoRef{}, "main",
		tree("sha-1", false, blob("package.json", 100), blob("node_modules/lodash/package.json", 100)))

	if len(reader.requests) != 1 || reader.requests[0] != "package.json" {
		t.Errorf("requested paths = %v, want only the repository's own manifest", reader.requests)
	}
}

// A truncated listing has to reach the stored fact as `complete = false`, since
// that column is the only thing standing between a partial read and a sweep.
func TestExtractFacts_StoresIncompleteWhenReadFails(t *testing.T) {
	store := newArchitectureStore()
	svc := newArchitectureService(store, newFakeEnqueuer())
	extractor := &stubExtractor{
		kind:    models.FactKindPackages,
		outcome: derive.CompleteOutcome(),
		wants:   func(path string) bool { return path == "go.mod" },
	}
	svc.extractors = []Extractor{extractor}

	reader := &stubFileReader{err: scm.ErrRateLimited}
	svc.ExtractFacts(context.Background(), factRepo(), reader, scm.RepoRef{}, "main",
		tree("sha-1", false, blob("go.mod", 100)))

	fact := store.facts[factKey("repo-a", models.FactKindPackages)]
	if fact == nil {
		t.Fatal("no fact stored, want one marked incomplete")
	}
	if fact.Complete {
		t.Error("fact.Complete = true, want false after a rate-limited read")
	}
}

func TestUnmarshalFactPayload_IncompleteRowOverridesStoredOutcome(t *testing.T) {
	// A row marked incomplete cannot be talked out of it by its own payload:
	// the column is what the sweep gate reads, so it wins.
	payload, err := json.Marshal(map[string]any{
		"data":    map[string]any{"published": []string{"github.com/org/shared"}},
		"outcome": derive.Outcome{Complete: true},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var data struct {
		Published []string `json:"published"`
	}
	outcome := UnmarshalFactPayload(&models.RepositoryFact{Payload: payload, Complete: false}, &data)

	if outcome.Complete {
		t.Error("outcome.Complete = true, want false")
	}
	if len(data.Published) != 1 || data.Published[0] != "github.com/org/shared" {
		t.Errorf("data.Published = %v, want the stored name", data.Published)
	}
}

// ── debounce ─────────────────────────────────────────────────────────────────

// Syncing 50 repositories must not queue 50 reconciliations. The task id is the
// organization, so the broker collapses the batch into one run that sees every
// fact it wrote.
func TestEnqueueDerivation_ManySyncsQueueOneReconciliation(t *testing.T) {
	enqueuer := newFakeEnqueuer()
	svc := newArchitectureService(newArchitectureStore(), enqueuer)

	for i := 0; i < 50; i++ {
		svc.EnqueueDerivation(context.Background(), testOrgID)
	}

	if len(enqueuer.enqueued) != 1 {
		t.Errorf("enqueued tasks = %d, want 1", len(enqueuer.enqueued))
	}
}

func TestEnqueueDerivation_DifferentOrganizationsAreNotCollapsed(t *testing.T) {
	enqueuer := newFakeEnqueuer()
	svc := newArchitectureService(newArchitectureStore(), enqueuer)

	svc.EnqueueDerivation(context.Background(), "org-1")
	svc.EnqueueDerivation(context.Background(), "org-2")

	if len(enqueuer.enqueued) != 2 {
		t.Errorf("enqueued tasks = %d, want 2", len(enqueuer.enqueued))
	}
}
