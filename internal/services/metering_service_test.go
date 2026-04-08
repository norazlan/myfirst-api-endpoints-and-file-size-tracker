package services

import (
	"sync"
	"testing"

	"metering-api/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// newTestDB creates an in-memory SQLite database for testing.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.EndpointMetric{}, &models.StorageRecord{}))
	return db
}

// TestMeteringService_IncrementAndGetMetrics verifies that incrementing different
// endpoints correctly updates per-endpoint counts and the global total.
func TestMeteringService_IncrementAndGetMetrics(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewMeteringService(db, 100)
	require.NoError(t, err)

	require.NoError(t, svc.IncrementEndpoint("/api/endpoint1"))
	require.NoError(t, svc.IncrementEndpoint("/api/endpoint1"))
	require.NoError(t, svc.IncrementEndpoint("/api/endpoint2"))

	metrics := svc.GetMetrics()
	assert.Equal(t, int64(2), metrics["/api/endpoint1"])
	assert.Equal(t, int64(1), metrics["/api/endpoint2"])
	assert.Equal(t, int64(3), svc.GetTotalRequests())
}

// TestMeteringService_RequestLimitEnforced verifies that once the global request
// limit is reached, further increments return ErrRequestLimitExceeded and the
// total count does not increase beyond the limit.
func TestMeteringService_RequestLimitEnforced(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewMeteringService(db, 5)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		require.NoError(t, svc.IncrementEndpoint("/api/test"))
	}

	// 6th request should fail
	err = svc.IncrementEndpoint("/api/test")
	assert.ErrorIs(t, err, ErrRequestLimitExceeded)

	// Total should remain at 5
	assert.Equal(t, int64(5), svc.GetTotalRequests())
}

// TestMeteringService_ConcurrentIncrements confirms thread safety by firing 1000
// concurrent goroutines, each incrementing the same endpoint, and asserting the
// final count matches exactly.
func TestMeteringService_ConcurrentIncrements(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewMeteringService(db, 10000)
	require.NoError(t, err)

	const goroutines = 1000
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = svc.IncrementEndpoint("/api/concurrent")
		}()
	}
	wg.Wait()

	assert.Equal(t, int64(goroutines), svc.GetTotalRequests())
	assert.Equal(t, int64(goroutines), svc.GetMetrics()["/api/concurrent"])
}

// TestMeteringService_FlushPersistsToDatabase verifies that calling Flush writes
// the in-memory counters to SQLite so they survive between service restarts.
func TestMeteringService_FlushPersistsToDatabase(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewMeteringService(db, 100)
	require.NoError(t, err)

	require.NoError(t, svc.IncrementEndpoint("/api/endpoint1"))
	require.NoError(t, svc.IncrementEndpoint("/api/endpoint1"))
	require.NoError(t, svc.IncrementEndpoint("/api/endpoint2"))

	// Flush to DB
	require.NoError(t, svc.Flush())

	// Verify in DB
	var metrics []models.EndpointMetric
	require.NoError(t, db.Find(&metrics).Error)
	assert.Len(t, metrics, 2)

	metricMap := make(map[string]int64)
	for _, m := range metrics {
		metricMap[m.Endpoint] = m.Count
	}
	assert.Equal(t, int64(2), metricMap["/api/endpoint1"])
	assert.Equal(t, int64(1), metricMap["/api/endpoint2"])
}

// TestMeteringService_HydratesFromDatabase verifies that a new MeteringService
// instance restores its counters from existing database records on startup.
func TestMeteringService_HydratesFromDatabase(t *testing.T) {
	db := newTestDB(t)

	// Seed DB with existing data
	db.Create(&models.EndpointMetric{Endpoint: "/api/existing", Count: 42})

	svc, err := NewMeteringService(db, 100)
	require.NoError(t, err)

	metrics := svc.GetMetrics()
	assert.Equal(t, int64(42), metrics["/api/existing"])
	assert.Equal(t, int64(42), svc.GetTotalRequests())
}

// TestMeteringService_FlushIsIdempotent ensures that calling Flush multiple times
// does not create duplicate rows — the upsert logic updates existing records.
func TestMeteringService_FlushIsIdempotent(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewMeteringService(db, 100)
	require.NoError(t, err)

	require.NoError(t, svc.IncrementEndpoint("/api/test"))
	require.NoError(t, svc.Flush())
	require.NoError(t, svc.Flush()) // Second flush should not duplicate

	var count int64
	db.Model(&models.EndpointMetric{}).Count(&count)
	assert.Equal(t, int64(1), count)
}
