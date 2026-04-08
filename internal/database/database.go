package database

import (
	"metering-api/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect opens a SQLite database at the given path and runs auto-migration
// for all application models.
func Connect(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	if err := db.AutoMigrate(&models.EndpointMetric{}, &models.StorageRecord{}); err != nil {
		return nil, err
	}

	return db, nil
}
