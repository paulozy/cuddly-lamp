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
	RepositoryID    string
	Kind            models.RepositoryRelationshipKind
	Source          models.RepositoryRelationshipSource
	IncludeMetadata bool
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

	resp := &models.RepositoryGraphResponse{
		Nodes: make([]models.RepositoryGraphNode, 0, len(repos)),
		Edges: make([]models.RepositoryGraphEdge, 0, len(relationships)),
	}
	for i := range repos {
		resp.Nodes = append(resp.Nodes, models.RepositoryToGraphNode(&repos[i]))
	}
	for i := range relationships {
		resp.Edges = append(resp.Edges, models.RepositoryRelationshipToGraphEdge(&relationships[i], filter.IncludeMetadata))
	}
	return resp, nil
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
