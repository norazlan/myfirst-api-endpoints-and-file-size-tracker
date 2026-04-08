package services

import (
	"errors"
	"sync"

	"metering-api/internal/models"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrRequestLimitExceeded is returned when the global request cap is reached.
var ErrRequestLimitExceeded = errors.New("request limit exceeded")

// MeteringService tracks API request counts per endpoint with thread-safe
// in-memory counters backed by periodic SQLite persistence.
type MeteringService struct {
	mu           sync.RWMutex
	counters     map[string]int64
	requestLimit int64
	db           *gorm.DB
}

// NewMeteringService creates a MeteringService and hydrates in-memory counters
// from the database so counts survive restarts.
func NewMeteringService(db *gorm.DB, requestLimit int64) (*MeteringService, error) {
	svc := &MeteringService{
		counters:     make(map[string]int64),
		requestLimit: requestLimit,
		db:           db,
	}

	// Hydrate from DB
	var metrics []models.EndpointMetric
	if err := db.Find(&metrics).Error; err != nil {
		return nil, err
	}
	for _, m := range metrics {
		svc.counters[m.Endpoint] = m.Count
	}

	return svc, nil
}

// IncrementEndpoint atomically increments the counter for the given endpoint.
// Returns ErrRequestLimitExceeded if the global total has reached the limit.
func (s *MeteringService) IncrementEndpoint(endpoint string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	total := s.totalLocked()
	if total >= s.requestLimit {
		return ErrRequestLimitExceeded
	}

	s.counters[endpoint]++
	return nil
}

// GetMetrics returns a snapshot of all endpoint counters.
func (s *MeteringService) GetMetrics() map[string]int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]int64, len(s.counters))
	for k, v := range s.counters {
		result[k] = v
	}
	return result
}

// GetTotalRequests returns the sum of all endpoint counters.
func (s *MeteringService) GetTotalRequests() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalLocked()
}

// Flush persists in-memory counters to the database. Should be called
// periodically and on graceful shutdown.
func (s *MeteringService) Flush() error {
	s.mu.RLock()
	snapshot := make(map[string]int64, len(s.counters))
	for k, v := range s.counters {
		snapshot[k] = v
	}
	s.mu.RUnlock()

	for endpoint, count := range snapshot {
		metric := models.EndpointMetric{
			Endpoint: endpoint,
			Count:    count,
		}
		err := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "endpoint"}},
			DoUpdates: clause.AssignmentColumns([]string{"count", "updated_at"}),
		}).Create(&metric).Error
		if err != nil {
			return err
		}
	}

	return nil
}

// GetRequestLimit returns the configured request limit.
func (s *MeteringService) GetRequestLimit() int64 {
	return s.requestLimit
}

// totalLocked returns the total request count. Caller must hold at least a read lock.
func (s *MeteringService) totalLocked() int64 {
	var total int64
	for _, v := range s.counters {
		total += v
	}
	return total
}
