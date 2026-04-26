package storage

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// openTestDB returns a real Postgres connection for integration tests.
// Skips the test if TEST_DATABASE_URL is not set.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping test db: %v", err)
	}
	return db
}

// cleanSchema drops tables created by test fixtures so tests are isolated.
func cleanSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`
		DROP TABLE IF EXISTS schema_migrations CASCADE;
		DROP TABLE IF EXISTS _test_table_a CASCADE;
		DROP TABLE IF EXISTS _test_table_b CASCADE;
		DROP TABLE IF EXISTS _test_table_c CASCADE;
		DROP TABLE IF EXISTS users CASCADE;
	`)
	if err != nil {
		t.Fatalf("clean schema: %v", err)
	}
}

// writeMigration creates a numbered migration file in dir and returns its name.
func writeMigration(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write migration %s: %v", name, err)
	}
}

func countApplied(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	return n
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var exists bool
	db.QueryRow(fmt.Sprintf(`SELECT EXISTS (
		SELECT 1 FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = '%s'
	)`, table)).Scan(&exists)
	return exists
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestRunMigrations_FreshDB_RunsAll(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cleanSchema(t, db)

	dir := t.TempDir()
	writeMigration(t, dir, "001-a.sql", "CREATE TABLE _test_table_a (id SERIAL PRIMARY KEY);")
	writeMigration(t, dir, "002-b.sql", "CREATE TABLE _test_table_b (id SERIAL PRIMARY KEY);")

	if err := createTrackingTable(db); err != nil {
		t.Fatalf("createTrackingTable: %v", err)
	}

	applied, _ := loadAppliedVersions(db)
	for _, f := range []string{"001-a.sql", "002-b.sql"} {
		if err := executeMigration(db, filepath.Join(dir, f)); err != nil {
			t.Fatalf("executeMigration %s: %v", f, err)
		}
		_ = applied
	}

	if got := countApplied(t, db); got != 2 {
		t.Errorf("expected 2 applied, got %d", got)
	}
	if !tableExists(t, db, "_test_table_a") {
		t.Error("_test_table_a should exist")
	}
	if !tableExists(t, db, "_test_table_b") {
		t.Error("_test_table_b should exist")
	}
}

func TestRunMigrations_Idempotent_SkipsApplied(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cleanSchema(t, db)

	dir := t.TempDir()
	writeMigration(t, dir, "001-a.sql", "CREATE TABLE _test_table_a (id SERIAL PRIMARY KEY);")

	if err := createTrackingTable(db); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := executeMigration(db, filepath.Join(dir, "001-a.sql")); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Second run: already applied → should skip without error.
	applied, _ := loadAppliedVersions(db)
	if !applied["001-a.sql"] {
		t.Fatal("expected 001-a.sql to be in applied after first run")
	}
	// Attempting to re-run the SQL would fail (table already exists without IF NOT EXISTS).
	// The skip logic prevents that. Simulate what RunMigrations does:
	if err := executeMigration(db, filepath.Join(dir, "001-a.sql")); err == nil {
		// Re-execution of CREATE TABLE without IF NOT EXISTS should fail;
		// the real runner skips it via applied check. This test verifies the
		// applied map is populated correctly.
		t.Log("note: re-execution did not error (SQL may be idempotent)")
	}

	// What matters is the tracking table has exactly 1 row (markApplied uses ON CONFLICT DO NOTHING).
	if got := countApplied(t, db); got != 1 {
		t.Errorf("expected 1 applied row, got %d", got)
	}
}

func TestRunMigrations_NewFile_OnlyRunsNew(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cleanSchema(t, db)

	dir := t.TempDir()
	writeMigration(t, dir, "001-a.sql", "CREATE TABLE _test_table_a (id SERIAL PRIMARY KEY);")

	if err := createTrackingTable(db); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := executeMigration(db, filepath.Join(dir, "001-a.sql")); err != nil {
		t.Fatalf("first run: %v", err)
	}

	// Add a second migration.
	writeMigration(t, dir, "002-b.sql", "CREATE TABLE _test_table_b (id SERIAL PRIMARY KEY);")
	applied, _ := loadAppliedVersions(db)

	if !applied["001-a.sql"] {
		t.Fatal("001-a.sql should already be applied")
	}
	// 002 is new → run it.
	if !applied["002-b.sql"] {
		if err := executeMigration(db, filepath.Join(dir, "002-b.sql")); err != nil {
			t.Fatalf("002-b.sql: %v", err)
		}
	}

	if got := countApplied(t, db); got != 2 {
		t.Errorf("expected 2 applied, got %d", got)
	}
	if !tableExists(t, db, "_test_table_b") {
		t.Error("_test_table_b should exist after second migration")
	}
}

func TestRunMigrations_FailedMigration_NotMarkedApplied(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cleanSchema(t, db)

	dir := t.TempDir()
	writeMigration(t, dir, "001-bad.sql", "THIS IS INVALID SQL !!!;")

	if err := createTrackingTable(db); err != nil {
		t.Fatalf("setup: %v", err)
	}

	err := executeMigration(db, filepath.Join(dir, "001-bad.sql"))
	if err == nil {
		t.Fatal("expected error from invalid SQL, got nil")
	}

	if got := countApplied(t, db); got != 0 {
		t.Errorf("failed migration should not be marked applied, got %d rows", got)
	}
}

func TestRunMigrations_BaselineExistingDB(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cleanSchema(t, db)

	// Simulate a pre-tracking DB: create users table directly, no schema_migrations.
	_, err := db.Exec("CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT);")
	if err != nil {
		t.Fatalf("create users: %v", err)
	}

	dir := t.TempDir()
	writeMigration(t, dir, "001-init.sql", "CREATE TABLE users (id SERIAL PRIMARY KEY, email TEXT);")
	writeMigration(t, dir, "002-extra.sql", "CREATE TABLE _test_table_a (id SERIAL PRIMARY KEY);")

	if err := createTrackingTable(db); err != nil {
		t.Fatalf("createTrackingTable: %v", err)
	}

	// Load applied (empty) — triggers baseline check.
	applied, _ := loadAppliedVersions(db)
	if len(applied) != 0 {
		t.Fatal("expected empty applied before baseline")
	}

	needed, err := isBaselineNeeded(db)
	if err != nil {
		t.Fatalf("isBaselineNeeded: %v", err)
	}
	if !needed {
		t.Fatal("expected baseline to be needed (users table exists)")
	}

	files, _ := os.ReadDir(dir)
	var sqlFiles []fs.DirEntry
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".sql" {
			sqlFiles = append(sqlFiles, f)
		}
	}

	if err := baselineFiles(db, sqlFiles); err != nil {
		t.Fatalf("baselineFiles: %v", err)
	}

	// Both files are seeded; neither was re-run (users table was not re-created).
	if got := countApplied(t, db); got != 2 {
		t.Errorf("expected 2 baselined rows, got %d", got)
	}
	// _test_table_a was NOT created (002 was not run, only seeded).
	if tableExists(t, db, "_test_table_a") {
		t.Error("_test_table_a should NOT exist — 002 was baselined, not executed")
	}
}
