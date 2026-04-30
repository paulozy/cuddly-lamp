package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetAnalysesByRepository_ReturnsOrderedPaginatedResults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE code_analyses (
			id TEXT PRIMARY KEY,
			repository_id TEXT,
			type TEXT,
			status TEXT,
			title TEXT DEFAULT '',
			created_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}

	now := time.Now().UTC()
	rows := []struct {
		id           string
		repositoryID string
		typ          models.AnalysisType
		createdAt    time.Time
	}{
		{id: "a1", repositoryID: "repo-1", typ: models.AnalysisTypeCodeReview, createdAt: now.Add(-2 * time.Hour)},
		{id: "a2", repositoryID: "repo-1", typ: models.AnalysisTypeSecurity, createdAt: now.Add(-1 * time.Hour)},
		{id: "a3", repositoryID: "repo-2", typ: models.AnalysisTypeArchitecture, createdAt: now},
	}
	for i, row := range rows {
		if err := db.Exec(
			`INSERT INTO code_analyses (id, repository_id, type, status, title, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			row.id, row.repositoryID, row.typ, models.AnalysisStatusCompleted, "", row.createdAt,
		).Error; err != nil {
			t.Fatalf("insert analysis %d: %v", i, err)
		}
	}

	repo := NewPostgresRepository(db)
	got, total, err := repo.GetAnalysesByRepository(context.Background(), "repo-1", 10, 0)
	if err != nil {
		t.Fatalf("GetAnalysesByRepository: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].ID != "a2" || got[1].ID != "a1" {
		t.Fatalf("order = %s, %s; want a2, a1", got[0].ID, got[1].ID)
	}

	got, total, err = repo.GetAnalysesByRepository(context.Background(), "repo-1", 1, 1)
	if err != nil {
		t.Fatalf("GetAnalysesByRepository paginated: %v", err)
	}
	if total != 2 {
		t.Fatalf("paginated total = %d, want 2", total)
	}
	if len(got) != 1 || got[0].ID != "a1" {
		t.Fatalf("paginated result = %+v, want a1", got)
	}

	got, total, err = repo.GetAnalysesByRepository(context.Background(), "repo-empty", 10, 0)
	if err != nil {
		t.Fatalf("GetAnalysesByRepository empty: %v", err)
	}
	if total != 0 {
		t.Fatalf("empty total = %d, want 0", total)
	}
	if len(got) != 0 {
		t.Fatalf("empty len = %d, want 0", len(got))
	}
}
