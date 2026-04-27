package storage

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/paulozy/idp-with-ai-backend/internal/config"
	"github.com/paulozy/idp-with-ai-backend/internal/utils"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type Database interface {
	GetDB() *gorm.DB
	Close() error
}

type database struct {
	db *gorm.DB
}

func New(cfg *config.DatabaseConfig) (*database, error) {
	utils.Info("Connecting to database", "host", cfg.Host, "dbname", cfg.Name)

	gormLog := gormlogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			LogLevel:                  gormlogger.Error,
			IgnoreRecordNotFoundError: true,
			Colorful:                  false,
		},
	)

	db, err := gorm.Open(postgres.Open(cfg.DSN), &gorm.Config{Logger: gormLog})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	utils.Info("Database connection established")

	if err := RunMigrations(db, "migrations"); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return &database{db: db}, nil
}

func (d *database) GetDB() *gorm.DB {
	return d.db
}

func (d *database) Close() error {
	sqlDB, err := d.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
