package storage

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/gorm"
)

func RunMigrations(db *gorm.DB, migrationsPath string) error {
	utils.Info("Running migrations", "path", migrationsPath)

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

	for _, file := range sqlFiles {
		if err := executeMigration(db, filepath.Join(migrationsPath, file.Name())); err != nil {
			return err
		}
	}

	utils.Info("Migrations completed successfully")
	return nil
}

func executeMigration(db *gorm.DB, filePath string) error {
	fileName := filepath.Base(filePath)
	utils.Info("Executing migration", "file", fileName)

	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read migration file %s: %w", fileName, err)
	}

	// pgx/v5 does not support multiple statements in a single Exec call.
	// Use the underlying *sql.DB to run the full file at once.
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql.DB for migration %s: %w", fileName, err)
	}

	if _, err := sqlDB.Exec(string(content)); err != nil {
		return fmt.Errorf("execute migration %s: %w", fileName, err)
	}

	utils.Info("Migration completed", "file", fileName)
	return nil
}
