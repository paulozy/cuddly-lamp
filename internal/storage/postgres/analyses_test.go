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

func TestPullRequestAnalysisLookups(t *testing.T) {
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
			pull_request_id INTEGER,
			created_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}

	now := time.Now().UTC()
	rows := []struct {
		id        string
		repoID    string
		typ       models.AnalysisType
		prID      any
		createdAt time.Time
	}{
		{id: "old-pr-42", repoID: "repo-1", typ: models.AnalysisTypeCodeReview, prID: 42, createdAt: now.Add(-2 * time.Hour)},
		{id: "new-pr-42", repoID: "repo-1", typ: models.AnalysisTypeCodeReview, prID: 42, createdAt: now.Add(-1 * time.Hour)},
		{id: "security-pr-42", repoID: "repo-1", typ: models.AnalysisTypeSecurity, prID: 42, createdAt: now},
		{id: "repo-2-pr-42", repoID: "repo-2", typ: models.AnalysisTypeCodeReview, prID: 42, createdAt: now},
		{id: "pr-7", repoID: "repo-1", typ: models.AnalysisTypeCodeReview, prID: 7, createdAt: now.Add(-30 * time.Minute)},
		{id: "no-pr", repoID: "repo-1", typ: models.AnalysisTypeCodeReview, prID: nil, createdAt: now},
	}
	for i, row := range rows {
		if err := db.Exec(
			`INSERT INTO code_analyses (id, repository_id, type, status, pull_request_id, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			row.id, row.repoID, row.typ, models.AnalysisStatusCompleted, row.prID, row.createdAt,
		).Error; err != nil {
			t.Fatalf("insert analysis %d: %v", i, err)
		}
	}

	repo := NewPostgresRepository(db)
	got, err := repo.GetLatestAnalysisForPullRequest(context.Background(), "repo-1", 42, models.AnalysisTypeCodeReview)
	if err != nil {
		t.Fatalf("GetLatestAnalysisForPullRequest: %v", err)
	}
	if got == nil || got.ID != "new-pr-42" {
		t.Fatalf("latest = %+v, want new-pr-42", got)
	}

	batch, err := repo.ListLatestAnalysesForPullRequests(context.Background(), "repo-1", []int{42, 7}, models.AnalysisTypeCodeReview)
	if err != nil {
		t.Fatalf("ListLatestAnalysesForPullRequests: %v", err)
	}
	if batch[42].ID != "new-pr-42" {
		t.Fatalf("batch[42] = %+v, want new-pr-42", batch[42])
	}
	if batch[7].ID != "pr-7" {
		t.Fatalf("batch[7] = %+v, want pr-7", batch[7])
	}

	empty, err := repo.GetLatestAnalysisForPullRequest(context.Background(), "repo-1", 99, models.AnalysisTypeCodeReview)
	if err != nil {
		t.Fatalf("GetLatestAnalysisForPullRequest empty: %v", err)
	}
	if empty != nil {
		t.Fatalf("empty = %+v, want nil", empty)
	}
}
