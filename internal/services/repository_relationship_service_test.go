package services

import (
	"context"
	"errors"
	"testing"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

type relationshipStore struct {
	storage.Repository
	repos         map[string]*models.Repository
	relationships map[string]*models.RepositoryRelationship
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
