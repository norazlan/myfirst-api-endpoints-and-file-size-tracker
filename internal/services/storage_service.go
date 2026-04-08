package services

import (
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sync"

	"metering-api/internal/models"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrStorageLimitExceeded is returned when a file upload would exceed the storage cap.
var ErrStorageLimitExceeded = errors.New("storage limit exceeded")

// StorageService tracks total storage used by uploaded files with thread-safe
// in-memory tracking backed by database records.
type StorageService struct {
	mu           sync.RWMutex
	totalUsed    int64
	storageLimit int64
	uploadDir    string
	db           *gorm.DB
}

// NewStorageService creates a StorageService and hydrates totalUsed from the database.
func NewStorageService(db *gorm.DB, storageLimit int64, uploadDir string) (*StorageService, error) {
	svc := &StorageService{
		storageLimit: storageLimit,
		uploadDir:    uploadDir,
		db:           db,
	}

	// Hydrate total from DB
	var total *int64
	if err := db.Model(&models.StorageRecord{}).Select("COALESCE(SUM(size), 0)").Scan(&total).Error; err != nil {
		return nil, err
	}
	if total != nil {
		svc.totalUsed = *total
	}

	// Ensure upload directory exists
	if err := os.MkdirAll(uploadDir, 0o750); err != nil {
		return nil, err
	}

	return svc, nil
}

// TrackUpload checks if adding a file of the given size would exceed the storage limit.
// If within limits, it records the file and updates the running total.
func (s *StorageService) TrackUpload(filename string, size int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.totalUsed+size > s.storageLimit {
		return ErrStorageLimitExceeded
	}

	record := models.StorageRecord{
		Filename: filename,
		Size:     size,
	}
	if err := s.db.Create(&record).Error; err != nil {
		return err
	}

	s.totalUsed += size
	return nil
}

// SaveFile writes the uploaded multipart file to the upload directory with a
// UUID-prefixed filename to prevent collisions. Returns the final filename.
func (s *StorageService) SaveFile(fh *multipart.FileHeader) (string, error) {
	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	uniqueName := fmt.Sprintf("%s_%s", uuid.New().String(), filepath.Base(fh.Filename))
	dstPath := filepath.Join(s.uploadDir, uniqueName)

	dst, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}

	return uniqueName, nil
}

// GetTotalStorage returns the current total storage used in bytes.
func (s *StorageService) GetTotalStorage() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalUsed
}

// GetStorageLimit returns the configured storage limit in bytes.
func (s *StorageService) GetStorageLimit() int64 {
	return s.storageLimit
}
