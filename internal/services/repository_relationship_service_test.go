package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

type relationshipStore struct {
	storage.Repository
	repos         map[string]*models.Repository
	relationships map[string]*models.RepositoryRelationship
	apis          []models.API
	resources     []models.Resource
	resourceLinks []models.RepositoryResource
}

func newRelationshipStore() *relationshipStore {
	return &relationshipStore{
		repos:         make(map[string]*models.Repository),
		relationships: make(map[string]*models.RepositoryRelationship),
	}
}

func (s *relationshipStore) GetRepository(_ context.Context, id string) (*models.Repository, error) {
	repo, ok := s.repos[id]
	if !ok {
		return nil, nil
	}
	return repo, nil
}

func (s *relationshipStore) ListRepositories(_ context.Context, filter *storage.RepositoryFilter) ([]models.Repository, int64, error) {
	var repos []models.Repository
	for _, repo := range s.repos {
		if filter.OrganizationID == "" || repo.OrganizationID == filter.OrganizationID {
			repos = append(repos, *repo)
		}
	}
	return repos, int64(len(repos)), nil
}

// The graph payload carries api nodes by default, so a store used for graph tests
// has to answer for them. An organization with no discovered APIs is the normal
// case and returns nothing.
func (s *relationshipStore) ListAPIs(_ context.Context, _ string) ([]models.API, error) {
	return s.apis, nil
}

func (s *relationshipStore) ListResources(_ context.Context, _ string) ([]models.Resource, error) {
	return s.resources, nil
}

func (s *relationshipStore) ListRepositoryResources(_ context.Context, _ string) ([]models.RepositoryResource, error) {
	return s.resourceLinks, nil
}

func (s *relationshipStore) CreateRepositoryRelationship(_ context.Context, rel *models.RepositoryRelationship) error {
	if rel.ID == "" {
		rel.ID = "rel-" + rel.SourceRepositoryID + "-" + rel.TargetRepositoryID + "-" + string(rel.Kind)
	}
	s.relationships[rel.ID] = rel
	return nil
}

func (s *relationshipStore) GetRepositoryRelationship(_ context.Context, id string) (*models.RepositoryRelationship, error) {
	rel, ok := s.relationships[id]
	if !ok {
		return nil, nil
	}
	return rel, nil
}

func (s *relationshipStore) UpdateRepositoryRelationship(_ context.Context, rel *models.RepositoryRelationship) error {
	s.relationships[rel.ID] = rel
	return nil
}

func (s *relationshipStore) DeleteRepositoryRelationship(_ context.Context, id string) error {
	delete(s.relationships, id)
	return nil
}

func (s *relationshipStore) ListRepositoryRelationships(_ context.Context, filter storage.RepositoryRelationshipFilter) ([]models.RepositoryRelationship, error) {
	var relationships []models.RepositoryRelationship
	for _, rel := range s.relationships {
		if filter.OrganizationID != "" && rel.OrganizationID != filter.OrganizationID {
			continue
		}
		if filter.RepositoryID != "" && rel.SourceRepositoryID != filter.RepositoryID && rel.TargetRepositoryID != filter.RepositoryID {
			continue
		}
		if filter.Kind != "" && rel.Kind != filter.Kind {
			continue
		}
		if filter.Source != "" && rel.Source != filter.Source {
			continue
		}
		relationships = append(relationships, *rel)
	}
	return relationships, nil
}

func TestRepositoryRelationshipService_GraphIncludesIndependentRepositories(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", Name: "a", OrganizationID: orgID}
	store.repos["repo-b"] = &models.Repository{ID: "repo-b", Name: "b", OrganizationID: orgID}
	store.repos["repo-c"] = &models.Repository{ID: "repo-c", Name: "c", OrganizationID: orgID}
	store.relationships["rel-1"] = &models.RepositoryRelationship{
		ID:                 "rel-1",
		OrganizationID:     orgID,
		SourceRepositoryID: "repo-a",
		TargetRepositoryID: "repo-b",
		Kind:               models.RepositoryRelationshipKindHTTP,
		Source:             models.RepositoryRelationshipSourceManual,
		Confidence:         1,
	}

	svc := NewRepositoryRelationshipService(store)
	graph, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{})
	if err != nil {
		t.Fatalf("GetGraph returned error: %v", err)
	}
	if len(graph.Nodes) != 3 {
		t.Fatalf("nodes = %d, want 3", len(graph.Nodes))
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(graph.Edges))
	}
}

func TestRepositoryRelationshipService_CreateAllowsMultipleRelationshipsBetweenSameRepositories(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: orgID}
	store.repos["repo-b"] = &models.Repository{ID: "repo-b", OrganizationID: orgID}

	svc := NewRepositoryRelationshipService(store)
	_, err := svc.CreateRelationship(context.Background(), orgID, ownerID, models.CreateRepositoryRelationshipRequest{
		SourceRepositoryID: "repo-a",
		TargetRepositoryID: "repo-b",
		Kind:               models.RepositoryRelationshipKindHTTP,
	})
	if err != nil {
		t.Fatalf("create http relationship: %v", err)
	}
	_, err = svc.CreateRelationship(context.Background(), orgID, ownerID, models.CreateRepositoryRelationshipRequest{
		SourceRepositoryID: "repo-a",
		TargetRepositoryID: "repo-b",
		Kind:               models.RepositoryRelationshipKindAsync,
	})
	if err != nil {
		t.Fatalf("create async relationship: %v", err)
	}
	if len(store.relationships) != 2 {
		t.Fatalf("relationships = %d, want 2", len(store.relationships))
	}
}

func TestRepositoryRelationshipService_CreateRejectsInvalidRelationships(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: orgID}
	store.repos["repo-b"] = &models.Repository{ID: "repo-b", OrganizationID: otherOrgID}
	svc := NewRepositoryRelationshipService(store)

	_, err := svc.CreateRelationship(context.Background(), orgID, ownerID, models.CreateRepositoryRelationshipRequest{
		SourceRepositoryID: "repo-a",
		TargetRepositoryID: "repo-a",
		Kind:               models.RepositoryRelationshipKindHTTP,
	})
	if !errors.Is(err, ErrInvalidRepositoryRelationship) {
		t.Fatalf("self relationship error = %v, want ErrInvalidRepositoryRelationship", err)
	}

	_, err = svc.CreateRelationship(context.Background(), orgID, ownerID, models.CreateRepositoryRelationshipRequest{
		SourceRepositoryID: "repo-a",
		TargetRepositoryID: "repo-b",
		Kind:               models.RepositoryRelationshipKindHTTP,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross org error = %v, want ErrForbidden", err)
	}
}

// A person who edits a derived edge takes ownership of it. Clearing the
// derivation key is what makes the next sweep abandon the row instead of
// fighting them for it — the alternative, duplicating the row, leaves two
// competing truths about the same dependency on the graph.
func TestUpdateRelationship_PromotesDerivedEdgeToHuman(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: "org-1"}
	store.repos["repo-b"] = &models.Repository{ID: "repo-b", OrganizationID: "org-1"}

	key := "libdep:v1:org/org-1"
	fingerprint := "fp-1"
	seenAt := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	store.relationships["rel-1"] = &models.RepositoryRelationship{
		ID:                    "rel-1",
		OrganizationID:        "org-1",
		SourceRepositoryID:    "repo-a",
		TargetRepositoryID:    "repo-b",
		Kind:                  models.RepositoryRelationshipKindLibrary,
		Source:                models.RepositoryRelationshipSourceManifest,
		Confidence:            1.0,
		DerivationKey:         &key,
		DerivationFingerprint: &fingerprint,
		LastSeenAt:            &seenAt,
	}

	svc := NewRepositoryRelationshipService(store)
	label := "checkout depends on shared"
	if _, err := svc.UpdateRelationship(context.Background(), "org-1", "rel-1",
		models.UpdateRepositoryRelationshipRequest{Label: &label}); err != nil {
		t.Fatalf("UpdateRelationship() error = %v, want nil", err)
	}

	got := store.relationships["rel-1"]
	if got.IsDerived() {
		t.Errorf("derivation_key = %v, want nil after a human edit", got.DerivationKey)
	}
	if got.DerivationFingerprint != nil || got.LastSeenAt != nil {
		t.Errorf("fingerprint = %v, last_seen_at = %v, want both nil", got.DerivationFingerprint, got.LastSeenAt)
	}
	if got.Label != label {
		t.Errorf("label = %q, want %q", got.Label, label)
	}
}

// ── the typed graph ──────────────────────────────────────────────────────────

func intPtr(v int) *int { return &v }

// Every node id is prefixed and carries its kind, so a client always knows what it
// is holding and where a click should route. Three tables sharing one bare-UUID
// namespace would invite collision and tell the frontend nothing.
func TestGetGraph_NodesArePrefixedAndTyped(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", Name: "orders", OrganizationID: orgID}
	store.apis = []models.API{{
		ID: "api-1", OrganizationID: orgID, RepositoryID: "repo-a",
		SpecPath: "openapi.yaml", Kind: models.APIKindOpenAPI,
		Title: "Orders API", Version: "1.4.0", OperationCount: intPtr(3),
	}}
	store.resources = []models.Resource{{
		ID: "res-1", OrganizationID: orgID, Engine: "postgresql",
		Host: "db.prod.internal", Port: intPtr(5432), Namespace: "orders",
		DisplayName: "postgresql @ db.prod.internal/orders",
	}}
	store.resourceLinks = []models.RepositoryResource{{
		ID: "link-1", OrganizationID: orgID, RepositoryID: "repo-a",
		ResourceID: "res-1", Confidence: 0.85,
	}}

	svc := NewRepositoryRelationshipService(store)
	graph, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{
		NodeKinds: []string{models.GraphNodeKindRepo, models.GraphNodeKindAPI, models.GraphNodeKindResource},
	})
	if err != nil {
		t.Fatalf("GetGraph() error = %v, want nil", err)
	}

	byID := map[string]models.RepositoryGraphNode{}
	for _, node := range graph.Nodes {
		byID[node.ID] = node
	}
	repo, ok := byID["repo:repo-a"]
	if !ok {
		t.Fatalf("nodes = %+v, want a repo:repo-a node", graph.Nodes)
	}
	if repo.Kind != models.GraphNodeKindRepo {
		t.Errorf("repo kind = %q, want %q", repo.Kind, models.GraphNodeKindRepo)
	}

	api, ok := byID["api:api-1"]
	if !ok {
		t.Fatalf("nodes = %+v, want an api:api-1 node", graph.Nodes)
	}
	if api.Kind != models.GraphNodeKindAPI || api.Version != "1.4.0" || api.SpecPath != "openapi.yaml" {
		t.Errorf("api node = %+v, want the typed api detail", api)
	}
	// nil and 0 are different facts here: nil means `$ref` made the count
	// unreliable, and the UI has to render a dash instead of a confident zero.
	if api.OperationCount == nil || *api.OperationCount != 3 {
		t.Errorf("operation_count = %v, want 3", api.OperationCount)
	}
	// The owning repository travels with the node so the drawer can link back
	// without another request.
	if api.RepositoryID != "repo-a" {
		t.Errorf("api repository_id = %q, want repo-a", api.RepositoryID)
	}

	resource, ok := byID["resource:res-1"]
	if !ok {
		t.Fatalf("nodes = %+v, want a resource:res-1 node", graph.Nodes)
	}
	if resource.Engine != "postgresql" || resource.Host != "db.prod.internal" {
		t.Errorf("resource node = %+v, want the locator", resource)
	}
	// is_scoped is always sent, because "local to this repository" versus "shared"
	// is the single most important thing a resource node communicates.
	if resource.IsScoped == nil {
		t.Fatal("is_scoped is nil, want it always populated on a resource node")
	}
	if *resource.IsScoped {
		t.Error("is_scoped = true, want false for a resource with a known host")
	}
}

// `provides` and `uses` are the two new edge kinds, and both endpoints are generic
// prefixed ids rather than repository ids.
func TestGetGraph_ProvidesAndUsesEdges(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: orgID}
	store.apis = []models.API{{
		ID: "api-1", OrganizationID: orgID, RepositoryID: "repo-a",
		SpecPath: "openapi.yaml", Kind: models.APIKindOpenAPI, Title: "Orders",
	}}
	store.resources = []models.Resource{{
		ID: "res-1", OrganizationID: orgID, Engine: "redis", DisplayName: "cache (local)",
	}}
	store.resourceLinks = []models.RepositoryResource{{
		ID: "link-1", OrganizationID: orgID, RepositoryID: "repo-a", ResourceID: "res-1", Confidence: 0.7,
	}}

	svc := NewRepositoryRelationshipService(store)
	graph, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{
		NodeKinds: []string{models.GraphNodeKindRepo, models.GraphNodeKindAPI, models.GraphNodeKindResource},
	})
	if err != nil {
		t.Fatalf("GetGraph() error = %v, want nil", err)
	}

	kinds := map[models.RepositoryRelationshipKind]models.RepositoryGraphEdge{}
	for _, edge := range graph.Edges {
		kinds[edge.Kind] = edge
	}
	provides, ok := kinds[models.RepositoryRelationshipKindProvides]
	if !ok {
		t.Fatalf("edges = %+v, want a provides edge", graph.Edges)
	}
	if provides.Source != "repo:repo-a" || provides.Target != "api:api-1" {
		t.Errorf("provides edge = %s → %s, want repo:repo-a → api:api-1", provides.Source, provides.Target)
	}
	uses, ok := kinds[models.RepositoryRelationshipKindUses]
	if !ok {
		t.Fatalf("edges = %+v, want a uses edge", graph.Edges)
	}
	if uses.Source != "repo:repo-a" || uses.Target != "resource:res-1" {
		t.Errorf("uses edge = %s → %s, want repo:repo-a → resource:res-1", uses.Source, uses.Target)
	}
	// The `uses` edge carries the join's confidence, because the strength of the
	// claim varies — a shared locator is stronger than a compose image.
	if uses.Confidence != 0.7 {
		t.Errorf("uses confidence = %v, want the join's 0.7", uses.Confidence)
	}
}

// Resources are off by default: they are the most numerous and least precise, so
// leading with them would make the first impression of the graph its noisiest
// layer.
func TestGetGraph_ResourcesAreOffByDefault(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: orgID}
	store.resources = []models.Resource{{ID: "res-1", OrganizationID: orgID, Engine: "redis"}}
	store.resourceLinks = []models.RepositoryResource{{
		ID: "link-1", OrganizationID: orgID, RepositoryID: "repo-a", ResourceID: "res-1", Confidence: 0.7,
	}}

	svc := NewRepositoryRelationshipService(store)
	graph, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{})
	if err != nil {
		t.Fatalf("GetGraph() error = %v, want nil", err)
	}
	for _, node := range graph.Nodes {
		if node.Kind == models.GraphNodeKindResource {
			t.Errorf("nodes = %+v, want resources off by default", graph.Nodes)
		}
	}
}

// An edge to a node that was filtered out draws a line into nowhere, which React
// Flow renders as a stub pointing at empty canvas.
func TestGetGraph_NodeKindFilterDropsDanglingEdges(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: orgID}
	store.apis = []models.API{{
		ID: "api-1", OrganizationID: orgID, RepositoryID: "repo-a",
		SpecPath: "openapi.yaml", Kind: models.APIKindOpenAPI, Title: "Orders",
	}}

	svc := NewRepositoryRelationshipService(store)
	graph, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{
		NodeKinds: []string{models.GraphNodeKindRepo},
	})
	if err != nil {
		t.Fatalf("GetGraph() error = %v, want nil", err)
	}
	for _, edge := range graph.Edges {
		if edge.Kind == models.RepositoryRelationshipKindProvides {
			t.Errorf("edges = %+v, want the provides edge dropped with its api node", graph.Edges)
		}
	}
}

// Repositories are never optional: every other node hangs off one, so hiding them
// would leave APIs and resources floating with no owner visible.
func TestGetGraph_RepoNodesAreAlwaysIncluded(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: orgID}

	svc := NewRepositoryRelationshipService(store)
	graph, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{
		NodeKinds: []string{models.GraphNodeKindAPI},
	})
	if err != nil {
		t.Fatalf("GetGraph() error = %v, want nil", err)
	}
	if len(graph.Nodes) != 1 || graph.Nodes[0].Kind != models.GraphNodeKindRepo {
		t.Errorf("nodes = %+v, want the repository node kept regardless", graph.Nodes)
	}
}

// The threshold does more for legibility than any per-edge encoding: it is what
// lets a person see "only what is certain" with one click.
func TestGetGraph_MinConfidenceHidesWeakEdges(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: orgID}
	store.repos["repo-b"] = &models.Repository{ID: "repo-b", OrganizationID: orgID}
	store.relationships["strong"] = &models.RepositoryRelationship{
		ID: "strong", OrganizationID: orgID,
		SourceRepositoryID: "repo-a", TargetRepositoryID: "repo-b",
		Kind: models.RepositoryRelationshipKindLibrary, Source: models.RepositoryRelationshipSourceManifest,
		Confidence: 1.0,
	}
	store.relationships["weak"] = &models.RepositoryRelationship{
		ID: "weak", OrganizationID: orgID,
		SourceRepositoryID: "repo-a", TargetRepositoryID: "repo-b",
		Kind: models.RepositoryRelationshipKindHTTP, Source: models.RepositoryRelationshipSourceConfig,
		Confidence: 0.75,
	}

	svc := NewRepositoryRelationshipService(store)
	graph, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{MinConfidence: 0.8})
	if err != nil {
		t.Fatalf("GetGraph() error = %v, want nil", err)
	}
	if len(graph.Edges) != 1 || graph.Edges[0].ID != "strong" {
		t.Errorf("edges = %+v, want only the edge at or above the threshold", graph.Edges)
	}
}

func TestGetGraph_RejectsUnknownNodeKindAndBadThreshold(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: orgID}
	svc := NewRepositoryRelationshipService(store)

	if _, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{
		NodeKinds: []string{"component"},
	}); !errors.Is(err, ErrInvalidRepositoryRelationship) {
		t.Errorf("unknown node kind error = %v, want ErrInvalidRepositoryRelationship", err)
	}
	if _, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{
		MinConfidence: 1.5,
	}); !errors.Is(err, ErrInvalidRepositoryRelationship) {
		t.Errorf("out-of-range threshold error = %v, want ErrInvalidRepositoryRelationship", err)
	}
}

// A derived edge must be distinguishable from a declaration in the payload, since
// that is the only input the renderer has for choosing the visual bucket.
func TestGetGraph_DerivedEdgeCarriesItsDerivationKey(t *testing.T) {
	store := newRelationshipStore()
	store.repos["repo-a"] = &models.Repository{ID: "repo-a", OrganizationID: orgID}
	store.repos["repo-b"] = &models.Repository{ID: "repo-b", OrganizationID: orgID}
	key := "libdep:v1:org/" + orgID
	store.relationships["derived"] = &models.RepositoryRelationship{
		ID: "derived", OrganizationID: orgID,
		SourceRepositoryID: "repo-a", TargetRepositoryID: "repo-b",
		Kind: models.RepositoryRelationshipKindLibrary, Source: models.RepositoryRelationshipSourceManifest,
		Confidence: 1.0, DerivationKey: &key,
	}
	store.relationships["declared"] = &models.RepositoryRelationship{
		ID: "declared", OrganizationID: orgID,
		SourceRepositoryID: "repo-b", TargetRepositoryID: "repo-a",
		Kind: models.RepositoryRelationshipKindHTTP, Source: models.RepositoryRelationshipSourceManual,
		Confidence: 1.0,
	}

	svc := NewRepositoryRelationshipService(store)
	graph, err := svc.GetGraph(context.Background(), orgID, RepositoryGraphFilter{})
	if err != nil {
		t.Fatalf("GetGraph() error = %v, want nil", err)
	}
	for _, edge := range graph.Edges {
		switch edge.ID {
		case "derived":
			if edge.DerivationKey == "" {
				t.Error("derived edge has no derivation_key, want the key that scopes its sweep")
			}
		case "declared":
			if edge.DerivationKey != "" {
				t.Errorf("declared edge derivation_key = %q, want empty", edge.DerivationKey)
			}
		}
	}
}
