package storage

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/gorm"
)

const createTrackingTableSQL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')
);`

func RunMigrations(db *gorm.DB, migrationsPath string) error {
	utils.Info("Running migrations", "path", migrationsPath)

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}

	if err := createTrackingTable(sqlDB); err != nil {
		return fmt.Errorf("create tracking table: %w", err)
	}

	files, err := os.ReadDir(migrationsPath)
	if err != nil {
		return fmt.Errorf("read migrations directory: %w", err)
	}

	var sqlFiles []fs.DirEntry
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".sql") {
			sqlFiles = append(sqlFiles, file)
		}
	}

	if len(sqlFiles) == 0 {
		utils.Warn("No migration files found", "path", migrationsPath)
		return nil
	}

	sort.Slice(sqlFiles, func(i, j int) bool {
		return sqlFiles[i].Name() < sqlFiles[j].Name()
	})

	applied, err := loadAppliedVersions(sqlDB)
	if err != nil {
		return fmt.Errorf("load applied versions: %w", err)
	}

	// Baseline: existing DB upgraded before tracking was introduced.
	if len(applied) == 0 {
		needed, err := isBaselineNeeded(sqlDB)
		if err != nil {
			return fmt.Errorf("check baseline: %w", err)
		}
		if needed {
			utils.Warn("Baseline mode: seeding existing migrations as already applied", "count", len(sqlFiles))
			return baselineFiles(sqlDB, sqlFiles)
		}
	}

	for _, file := range sqlFiles {
		if applied[file.Name()] {
			utils.Info("Skipping already applied migration", "file", file.Name())
			continue
		}
		if err := executeMigration(sqlDB, filepath.Join(migrationsPath, file.Name())); err != nil {
			return err
		}
	}

	utils.Info("Migrations completed successfully")
	return nil
}

func createTrackingTable(db *sql.DB) error {
	_, err := db.Exec(createTrackingTableSQL)
	return err
}

func loadAppliedVersions(db *sql.DB) (map[string]bool, error) {
	rows, err := db.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

func markApplied(db *sql.DB, version string) error {
	_, err := db.Exec(
		"INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING",
		version,
	)
	return err
}

// isBaselineNeeded returns true when schema_migrations is empty but the database
// was already migrated before tracking was introduced (users table exists).
func isBaselineNeeded(db *sql.DB) (bool, error) {
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'users'
		)`).Scan(&exists)
	return exists, err
}

func baselineFiles(db *sql.DB, files []fs.DirEntry) error {
	for _, file := range files {
		if err := markApplied(db, file.Name()); err != nil {
			return fmt.Errorf("baseline %s: %w", file.Name(), err)
		}
		utils.Info("Baselined migration", "file", file.Name())
	}
	return nil
}

func executeMigration(db *sql.DB, filePath string) error {
	fileName := filepath.Base(filePath)
	utils.Info("Executing migration", "file", fileName)

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read migration file %s: %w", fileName, err)
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction for %s: %w", fileName, err)
	}

	if _, err := tx.Exec(string(content)); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("execute migration %s: %w", fileName, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", fileName, err)
	}

	if err := markApplied(db, fileName); err != nil {
		return fmt.Errorf("mark applied %s: %w", fileName, err)
	}

	utils.Info("Migration completed", "file", fileName)
	return nil
}
