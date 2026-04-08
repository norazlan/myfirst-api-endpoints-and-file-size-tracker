package services

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStorageService_TrackUpload verifies that tracking successive uploads
// correctly accumulates the total storage used.
func TestStorageService_TrackUpload(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewStorageService(db, 1024, t.TempDir())
	require.NoError(t, err)

	require.NoError(t, svc.TrackUpload("file1.txt", 500))
	assert.Equal(t, int64(500), svc.GetTotalStorage())

	require.NoError(t, svc.TrackUpload("file2.txt", 200))
	assert.Equal(t, int64(700), svc.GetTotalStorage())
}

// TestStorageService_StorageLimitEnforced verifies that an upload exceeding the
// configured storage limit is rejected with ErrStorageLimitExceeded and the total
// storage remains unchanged.
func TestStorageService_StorageLimitEnforced(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewStorageService(db, 1000, t.TempDir())
	require.NoError(t, err)

	require.NoError(t, svc.TrackUpload("file1.txt", 800))

	// This upload would exceed the 1000 byte limit
	err = svc.TrackUpload("file2.txt", 300)
	assert.ErrorIs(t, err, ErrStorageLimitExceeded)

	// Total should remain at 800
	assert.Equal(t, int64(800), svc.GetTotalStorage())
}

// TestStorageService_ExactLimitAllowed verifies that an upload filling storage
// exactly to the limit succeeds, while any subsequent upload (even 1 byte) is rejected.
func TestStorageService_ExactLimitAllowed(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewStorageService(db, 1000, t.TempDir())
	require.NoError(t, err)

	// Upload exactly up to the limit
	require.NoError(t, svc.TrackUpload("file1.txt", 1000))
	assert.Equal(t, int64(1000), svc.GetTotalStorage())

	// Any additional upload should fail
	err = svc.TrackUpload("file2.txt", 1)
	assert.ErrorIs(t, err, ErrStorageLimitExceeded)
}

// TestStorageService_ConcurrentUploads confirms thread safety by running 100
// concurrent goroutines, each tracking a fixed-size upload, and asserting the
// total equals the expected cumulative size.
func TestStorageService_ConcurrentUploads(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewStorageService(db, 1_000_000, t.TempDir())
	require.NoError(t, err)

	const goroutines = 100
	const sizeEach = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer wg.Done()
			_ = svc.TrackUpload("concurrent_file.txt", sizeEach)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int64(goroutines*sizeEach), svc.GetTotalStorage())
}

// TestStorageService_HydratesFromDatabase verifies that a new StorageService
// instance restores the total storage used from existing database records.
func TestStorageService_HydratesFromDatabase(t *testing.T) {
	db := newTestDB(t)

	// First service instance tracks some uploads
	svc1, err := NewStorageService(db, 10000, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, svc1.TrackUpload("file1.txt", 500))
	require.NoError(t, svc1.TrackUpload("file2.txt", 300))

	// Second instance should hydrate from DB
	svc2, err := NewStorageService(db, 10000, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, int64(800), svc2.GetTotalStorage())
}

// TestStorageService_GetStorageLimit verifies that GetStorageLimit returns the
// configured storage limit value passed during service construction.
func TestStorageService_GetStorageLimit(t *testing.T) {
	db := newTestDB(t)
	svc, err := NewStorageService(db, 1073741824, t.TempDir())
	require.NoError(t, err)

	assert.Equal(t, int64(1073741824), svc.GetStorageLimit())
}
