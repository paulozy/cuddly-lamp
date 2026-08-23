package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

var (
	ErrRepositoryRelationshipNotFound = errors.New("repository relationship not found")
	ErrInvalidRepositoryRelationship  = errors.New("invalid repository relationship")
)

type RepositoryRelationshipService struct {
	repo storage.Repository
}

func NewRepositoryRelationshipService(repo storage.Repository) *RepositoryRelationshipService {
	return &RepositoryRelationshipService{repo: repo}
}

type RepositoryGraphFilter struct {
	RepositoryID string
	Kind         models.RepositoryRelationshipKind
	Source       models.RepositoryRelationshipSource
	// NodeKinds selects which node types the payload carries. Empty means the
	// default set.
	//
	// It exists because APIs and Resources can multiply the node count several
	// times over, and GetGraph has never paginated — a half-drawn graph is worse
	// than none, so paginating was never an option. Filtering by node type is the
	// answer instead, with the toolbar toggling it.
	NodeKinds       []string
	MinConfidence   float64
	IncludeMetadata bool
}

// defaultGraphNodeKinds is what a client that asks for nothing specific gets.
//
// Resources are off by default deliberately: they are the most numerous and the
// least precise, so leading with them would make the first impression of the graph
// its noisiest layer.
var defaultGraphNodeKinds = []string{models.GraphNodeKindRepo, models.GraphNodeKindAPI}

func (f RepositoryGraphFilter) nodeKindSet() map[string]bool {
	kinds := f.NodeKinds
	if len(kinds) == 0 {
		kinds = defaultGraphNodeKinds
	}
	set := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		set[kind] = true
	}
	// Repositories are never optional. Every other node hangs off one, so hiding
	// them would leave APIs and resources floating with no owner visible.
	set[models.GraphNodeKindRepo] = true
	return set
}

// IsValidGraphNodeKind reports whether a node kind exists.
func IsValidGraphNodeKind(kind string) bool {
	switch kind {
	case models.GraphNodeKindRepo, models.GraphNodeKindAPI, models.GraphNodeKindResource:
		return true
	default:
		return false
	}
}

func (s *RepositoryRelationshipService) GetGraph(ctx context.Context, organizationID string, filter RepositoryGraphFilter) (*models.RepositoryGraphResponse, error) {
	if filter.Kind != "" && !models.IsValidRepositoryRelationshipKind(filter.Kind) {
		return nil, ErrInvalidRepositoryRelationship
	}
	if filter.Source != "" && !models.IsValidRepositoryRelationshipSource(filter.Source) {
		return nil, ErrInvalidRepositoryRelationship
	}
	if filter.RepositoryID != "" {
		repo, err := s.fetchRepositoryInOrganization(ctx, filter.RepositoryID, organizationID)
		if err != nil {
			return nil, err
		}
		if repo == nil {
			return nil, ErrRepositoryNotFound
		}
	}

	for _, kind := range filter.NodeKinds {
		if !IsValidGraphNodeKind(kind) {
			return nil, ErrInvalidRepositoryRelationship
		}
	}
	if filter.MinConfidence < 0 || filter.MinConfidence > 1 {
		return nil, ErrInvalidRepositoryRelationship
	}

	repos, _, err := s.repo.ListRepositories(ctx, &storage.RepositoryFilter{
		OrganizationID: organizationID,
	})
	if err != nil {
		return nil, fmt.Errorf("list graph repositories: %w", err)
	}
	relationships, err := s.repo.ListRepositoryRelationships(ctx, storage.RepositoryRelationshipFilter{
		OrganizationID: organizationID,
		RepositoryID:   filter.RepositoryID,
		Kind:           filter.Kind,
		Source:         filter.Source,
	})
	if err != nil {
		return nil, fmt.Errorf("list graph relationships: %w", err)
	}

	wantKind := filter.nodeKindSet()
	resp := &models.RepositoryGraphResponse{
		Nodes: make([]models.RepositoryGraphNode, 0, len(repos)),
		Edges: make([]models.RepositoryGraphEdge, 0, len(relationships)),
	}
	// present is every node id the payload will carry, so a dangling edge can be
	// dropped. An edge to a node that was filtered out draws a line into nowhere,
	// which React Flow renders as a stub pointing at empty canvas.
	present := make(map[string]bool, len(repos))

	for i := range repos {
		node := models.RepositoryToGraphNode(&repos[i])
		resp.Nodes = append(resp.Nodes, node)
		present[node.ID] = true
	}

	if wantKind[models.GraphNodeKindAPI] {
		apis, err := s.repo.ListAPIs(ctx, organizationID)
		if err != nil {
			return nil, fmt.Errorf("list graph apis: %w", err)
		}
		for i := range apis {
			node := models.APIToGraphNode(&apis[i])
			resp.Nodes = append(resp.Nodes, node)
			present[node.ID] = true
			resp.Edges = append(resp.Edges, models.APIProvidesEdge(&apis[i]))
		}
	}

	if wantKind[models.GraphNodeKindResource] {
		resources, err := s.repo.ListResources(ctx, organizationID)
		if err != nil {
			return nil, fmt.Errorf("list graph resources: %w", err)
		}
		for i := range resources {
			node := models.ResourceToGraphNode(&resources[i])
			resp.Nodes = append(resp.Nodes, node)
			present[node.ID] = true
		}
		links, err := s.repo.ListRepositoryResources(ctx, organizationID)
		if err != nil {
			return nil, fmt.Errorf("list graph repository resources: %w", err)
		}
		for i := range links {
			resp.Edges = append(resp.Edges, models.ResourceUsesEdge(&links[i], filter.IncludeMetadata))
		}
	}

	for i := range relationships {
		resp.Edges = append(resp.Edges, models.RepositoryRelationshipToGraphEdge(&relationships[i], filter.IncludeMetadata))
	}

	resp.Edges = pruneEdges(resp.Edges, present, filter.MinConfidence)
	return resp, nil
}

// pruneEdges drops edges whose endpoints are absent or whose confidence is below
// the requested floor.
//
// The confidence floor is applied here rather than in SQL because the same value
// has to cut across three sources — relationships, `provides` and `uses` — and
// pushing it into three queries would mean three places for the threshold to drift.
func pruneEdges(edges []models.RepositoryGraphEdge, present map[string]bool, minConfidence float64) []models.RepositoryGraphEdge {
	kept := make([]models.RepositoryGraphEdge, 0, len(edges))
	for _, edge := range edges {
		if !present[edge.Source] || !present[edge.Target] {
			continue
		}
		if edge.Confidence < minConfidence {
			continue
		}
		kept = append(kept, edge)
	}
	return kept
}

func (s *RepositoryRelationshipService) CreateRelationship(ctx context.Context, organizationID, userID string, req models.CreateRepositoryRelationshipRequest) (*models.RepositoryRelationshipResponse, error) {
	if !models.IsValidRepositoryRelationshipKind(req.Kind) || req.SourceRepositoryID == req.TargetRepositoryID {
		return nil, ErrInvalidRepositoryRelationship
	}
	if _, err := s.requireRepositoryInOrganization(ctx, req.SourceRepositoryID, organizationID); err != nil {
		return nil, err
	}
	if _, err := s.requireRepositoryInOrganization(ctx, req.TargetRepositoryID, organizationID); err != nil {
		return nil, err
	}

	rel := &models.RepositoryRelationship{
		OrganizationID:     organizationID,
		SourceRepositoryID: req.SourceRepositoryID,
		TargetRepositoryID: req.TargetRepositoryID,
		Kind:               req.Kind,
		Label:              req.Label,
		Description:        req.Description,
		Source:             models.RepositoryRelationshipSourceManual,
		Confidence:         1.0,
		Metadata:           req.Metadata,
		CreatedByUserID:    userID,
	}
	if rel.Metadata == nil {
		rel.Metadata = map[string]interface{}{}
	}
	if err := s.repo.CreateRepositoryRelationship(ctx, rel); err != nil {
		return nil, fmt.Errorf("create repository relationship: %w", err)
	}
	return models.RepositoryRelationshipToResponse(rel), nil
}

func (s *RepositoryRelationshipService) UpdateRelationship(ctx context.Context, organizationID, relationshipID string, req models.UpdateRepositoryRelationshipRequest) (*models.RepositoryRelationshipResponse, error) {
	rel, err := s.requireRelationshipInOrganization(ctx, relationshipID, organizationID)
	if err != nil {
		return nil, err
	}
	if req.Kind != nil {
		if !models.IsValidRepositoryRelationshipKind(*req.Kind) {
			return nil, ErrInvalidRepositoryRelationship
		}
		rel.Kind = *req.Kind
	}
	if req.Label != nil {
		rel.Label = *req.Label
	}
	if req.Description != nil {
		rel.Description = *req.Description
	}
	if req.Confidence != nil {
		if *req.Confidence < 0 || *req.Confidence > 1 {
			return nil, ErrInvalidRepositoryRelationship
		}
		rel.Confidence = *req.Confidence
	}
	if req.Metadata != nil {
		rel.Metadata = req.Metadata
	}
	// A person editing a derived edge takes ownership of it. Promoting instead
	// of copying is what keeps the graph from holding two competing truths about
	// the same dependency, and it is what makes the next sweep leave the row
	// alone — a NULL derivation key never matches the sweep's `= $1`.
	rel.Promote()
	if err := s.repo.UpdateRepositoryRelationship(ctx, rel); err != nil {
		return nil, fmt.Errorf("update repository relationship: %w", err)
	}
	return models.RepositoryRelationshipToResponse(rel), nil
}

func (s *RepositoryRelationshipService) DeleteRelationship(ctx context.Context, organizationID, relationshipID string) error {
	if _, err := s.requireRelationshipInOrganization(ctx, relationshipID, organizationID); err != nil {
		return err
	}
	if err := s.repo.DeleteRepositoryRelationship(ctx, relationshipID); err != nil {
		return fmt.Errorf("delete repository relationship: %w", err)
	}
	return nil
}

func (s *RepositoryRelationshipService) requireRepositoryInOrganization(ctx context.Context, repoID, organizationID string) (*models.Repository, error) {
	repo, err := s.fetchRepositoryInOrganization(ctx, repoID, organizationID)
	if err != nil {
		return nil, err
	}
	if repo == nil {
		return nil, ErrRepositoryNotFound
	}
	return repo, nil
}

func (s *RepositoryRelationshipService) fetchRepositoryInOrganization(ctx context.Context, repoID, organizationID string) (*models.Repository, error) {
	repo, err := s.repo.GetRepository(ctx, repoID)
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}
	if repo == nil {
		return nil, nil
	}
	if repo.OrganizationID != organizationID {
		return nil, ErrForbidden
	}
	return repo, nil
}

func (s *RepositoryRelationshipService) requireRelationshipInOrganization(ctx context.Context, relationshipID, organizationID string) (*models.RepositoryRelationship, error) {
	rel, err := s.repo.GetRepositoryRelationship(ctx, relationshipID)
	if err != nil {
		return nil, fmt.Errorf("get repository relationship: %w", err)
	}
	if rel == nil {
		return nil, ErrRepositoryRelationshipNotFound
	}
	if rel.OrganizationID != organizationID {
		return nil, ErrForbidden
	}
	return rel, nil
}
