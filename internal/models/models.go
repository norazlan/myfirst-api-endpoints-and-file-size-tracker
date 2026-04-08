package models

import "time"

// EndpointMetric stores the request count for a specific API endpoint.
type EndpointMetric struct {
	ID        uint   `gorm:"primaryKey"`
	Endpoint  string `gorm:"uniqueIndex;not null"`
	Count     int64  `gorm:"not null;default:0"`
	UpdatedAt time.Time
}

// StorageRecord tracks an individual uploaded file and its size in bytes.
type StorageRecord struct {
	ID        uint   `gorm:"primaryKey"`
	Filename  string `gorm:"not null"`
	Size      int64  `gorm:"not null"`
	CreatedAt time.Time
}
