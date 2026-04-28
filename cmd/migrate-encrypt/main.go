// migrate-encrypt is a one-shot tool that reads plaintext oauth_connections.access_token
// and webhook_configs.secret rows, encrypts them, and writes back the encrypted bytes
// as hex strings into the TEXT columns. Run BEFORE migration 006.
//
// After this tool completes, apply 006-encrypt-sensitive-fields.sql, which converts
// the hex strings to bytea.
//
// The tool is idempotent: rows where the hex value decodes and decrypts successfully
// with the current key are skipped.
//
// Usage: ENCRYPTION_KEY=<base64-32-bytes> DB_* go run ./cmd/migrate-encrypt
package main

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/gorm"

	"github.com/paulozy/idp-with-ai-backend/internal/config"
	appcrypto "github.com/paulozy/idp-with-ai-backend/internal/crypto"
	"github.com/paulozy/idp-with-ai-backend/internal/storage"
)

func main() {
	godotenv.Load()

	cfg := config.Load()

	rawKey, err := base64.StdEncoding.DecodeString(cfg.Server.EncryptionKey)
	if err != nil || len(rawKey) != 32 {
		fmt.Fprintln(os.Stderr, "ENCRYPTION_KEY must be a base64-encoded 32-byte value (generate with: openssl rand -base64 32)")
		os.Exit(1)
	}

	c, err := appcrypto.New(rawKey, 0x01)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cipher init: %v\n", err)
		os.Exit(1)
	}

	db, err := storage.New(&cfg.Database)
	if err != nil {
		fmt.Fprintf(os.Stderr, "db init: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	gdb := db.GetDB()

	fmt.Println("==> Encrypting oauth_connections.access_token")
	if err := backfill(gdb, c, "oauth_connections", "id", "access_token"); err != nil {
		fmt.Fprintf(os.Stderr, "oauth_connections: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("==> Encrypting webhook_configs.secret")
	if err := backfill(gdb, c, "webhook_configs", "id", "secret"); err != nil {
		fmt.Fprintf(os.Stderr, "webhook_configs: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Done. Apply migration 006-encrypt-sensitive-fields.sql next.")
}

type row struct {
	ID    string
	Value *string
}

// backfill reads each non-NULL value from table.valueColumn, encrypts it
// (skipping rows that are already encrypted), and writes the hex-encoded
// ciphertext back into the same TEXT column.
func backfill(db *gorm.DB, c *appcrypto.Cipher, table, idColumn, valueColumn string) error {
	var rows []row
	query := fmt.Sprintf(
		"SELECT %s AS id, %s AS value FROM %s WHERE %s IS NOT NULL",
		idColumn, valueColumn, table, valueColumn,
	)
	if err := db.Raw(query).Scan(&rows).Error; err != nil {
		return fmt.Errorf("read rows: %w", err)
	}
	fmt.Printf("  found %d non-NULL rows\n", len(rows))

	encrypted, skipped := 0, 0
	for _, r := range rows {
		if r.Value == nil || *r.Value == "" {
			continue
		}

		// Skip already-encrypted rows: try to hex-decode then decrypt.
		if blob, err := hex.DecodeString(*r.Value); err == nil {
			if _, err := c.Decrypt(blob); err == nil {
				skipped++
				continue
			}
		}

		blob, err := c.Encrypt([]byte(*r.Value))
		if err != nil {
			return fmt.Errorf("encrypt row %s: %w", r.ID, err)
		}

		update := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s = $2", table, valueColumn, idColumn)
		if err := db.Exec(update, hex.EncodeToString(blob), r.ID).Error; err != nil {
			return fmt.Errorf("update row %s: %w", r.ID, err)
		}
		encrypted++
	}
	fmt.Printf("  encrypted: %d, skipped (already done): %d\n", encrypted, skipped)
	return nil
}
