package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"metering-api/internal/models"
	"metering-api/internal/services"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.EndpointMetric{}, &models.StorageRecord{}))
	return db
}

func setupApp(t *testing.T, requestLimit int64, storageLimit int64) (*fiber.App, *services.MeteringService, *services.StorageService) {
	t.Helper()
	db := newTestDB(t)

	ms, err := services.NewMeteringService(db, requestLimit)
	require.NoError(t, err)

	ss, err := services.NewStorageService(db, storageLimit, t.TempDir())
	require.NoError(t, err)

	mh := NewMeteringHandler(ms)
	sh := NewStorageHandler(ss)

	app := fiber.New()

	api := app.Group("/api")
	api.Post("/endpoint1", mh.HandleEndpoint1)
	api.Post("/endpoint2", mh.HandleEndpoint2)
	api.Get("/metrics", mh.GetMetrics)

	app.Post("/upload", sh.UploadFile)
	app.Get("/storage", sh.GetStorage)

	return app, ms, ss
}

// TestPostEndpoint1_Success verifies that a POST to /api/endpoint1 returns 200 OK
// with the expected success message.
func TestPostEndpoint1_Success(t *testing.T) {
	app, _, _ := setupApp(t, 100, 1024)

	req := httptest.NewRequest(http.MethodPost, "/api/endpoint1", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	body := parseJSON(t, resp)
	assert.Equal(t, "request to endpoint1 processed successfully", body["message"])
}

// TestPostEndpoint2_Success verifies that a POST to /api/endpoint2 returns 200 OK
// with the expected success message.
func TestPostEndpoint2_Success(t *testing.T) {
	app, _, _ := setupApp(t, 100, 1024)

	req := httptest.NewRequest(http.MethodPost, "/api/endpoint2", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	body := parseJSON(t, resp)
	assert.Equal(t, "request to endpoint2 processed successfully", body["message"])
}

// TestGetMetrics_ReturnsCorrectCounts verifies that GET /api/metrics returns accurate
// per-endpoint counts, the correct total, and that /api/metrics itself is NOT tracked.
func TestGetMetrics_ReturnsCorrectCounts(t *testing.T) {
	app, _, _ := setupApp(t, 100, 1024)

	// Make 3 requests to endpoint1
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/endpoint1", nil)
		_, err := app.Test(req)
		require.NoError(t, err)
	}

	// Make 2 requests to endpoint2
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/endpoint2", nil)
		_, err := app.Test(req)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	body := parseJSON(t, resp)

	// /api/metrics is NOT metered, so total should be exactly 5
	assert.Equal(t, float64(5), body["total_requests"])

	endpoints, ok := body["endpoints"].(map[string]interface{})
	require.True(t, ok, "endpoints should be a map")
	assert.Equal(t, float64(3), endpoints["/api/endpoint1"])
	assert.Equal(t, float64(2), endpoints["/api/endpoint2"])
	// /api/metrics should not appear in tracked endpoints
	_, exists := endpoints["/api/metrics"]
	assert.False(t, exists)
}

// TestPostEndpoint1_RequestLimitExceeded verifies that once the global request limit
// is exhausted, the next request receives a 429 Too Many Requests response.
func TestPostEndpoint1_RequestLimitExceeded(t *testing.T) {
	app, _, _ := setupApp(t, 3, 1024)

	// Exhaust the limit
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/endpoint1", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
	}

	// 4th request should be rejected
	req := httptest.NewRequest(http.MethodPost, "/api/endpoint1", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusTooManyRequests, resp.StatusCode)

	body := parseJSON(t, resp)
	assert.Equal(t, "request limit exceeded", body["error"])
}

// TestUploadFile_Success verifies that uploading a valid multipart file returns
// 201 Created with the filename and correct byte size in the response.
func TestUploadFile_Success(t *testing.T) {
	app, _, _ := setupApp(t, 100, 1_000_000)

	body, contentType := createMultipartFile(t, "file", "testfile.txt", []byte("hello world"))
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

	result := parseJSON(t, resp)
	assert.Equal(t, "file uploaded successfully", result["message"])
	assert.Equal(t, float64(11), result["size"]) // "hello world" = 11 bytes
}

// TestUploadFile_NoFile verifies that a POST to /upload without an attached file
// returns 400 Bad Request.
func TestUploadFile_NoFile(t *testing.T) {
	app, _, _ := setupApp(t, 100, 1_000_000)

	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req.Header.Set("Content-Type", "multipart/form-data")

	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusBadRequest, resp.StatusCode)

	body := parseJSON(t, resp)
	assert.Equal(t, "file is required", body["error"])
}

// TestUploadFile_StorageLimitExceeded verifies that an upload that would push total
// storage beyond the configured limit is rejected with 413 Payload Too Large.
func TestUploadFile_StorageLimitExceeded(t *testing.T) {
	app, _, _ := setupApp(t, 100, 20) // 20 byte storage limit

	// Upload a file that fits
	body1, ct1 := createMultipartFile(t, "file", "small.txt", []byte("hello")) // 5 bytes
	req1 := httptest.NewRequest(http.MethodPost, "/upload", body1)
	req1.Header.Set("Content-Type", ct1)
	resp1, err := app.Test(req1)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusCreated, resp1.StatusCode)

	// Upload a file that exceeds the limit
	bigData := bytes.Repeat([]byte("x"), 20)
	body2, ct2 := createMultipartFile(t, "file", "big.txt", bigData)
	req2 := httptest.NewRequest(http.MethodPost, "/upload", body2)
	req2.Header.Set("Content-Type", ct2)
	resp2, err := app.Test(req2)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusRequestEntityTooLarge, resp2.StatusCode)

	result := parseJSON(t, resp2)
	assert.Equal(t, "storage limit exceeded", result["error"])
}

// TestGetStorage_ReturnsUsageInfo verifies that GET /storage returns the correct
// total used bytes, the storage limit, and a positive usage percentage after an upload.
func TestGetStorage_ReturnsUsageInfo(t *testing.T) {
	app, _, _ := setupApp(t, 100, 1_000_000)

	// Upload a file first
	body, ct := createMultipartFile(t, "file", "test.txt", []byte("hello world"))
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", ct)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

	// Check storage
	req2 := httptest.NewRequest(http.MethodGet, "/storage", nil)
	resp2, err := app.Test(req2)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp2.StatusCode)

	result := parseJSON(t, resp2)
	assert.Equal(t, float64(11), result["total_storage_used"])
	assert.Equal(t, float64(1_000_000), result["storage_limit"])
	assert.Greater(t, result["usage_percentage"].(float64), float64(0))
}

// TestGetStorage_EmptyInitially verifies that GET /storage returns zero usage and
// zero percentage when no files have been uploaded yet.
func TestGetStorage_EmptyInitially(t *testing.T) {
	app, _, _ := setupApp(t, 100, 1_000_000)

	req := httptest.NewRequest(http.MethodGet, "/storage", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	result := parseJSON(t, resp)
	assert.Equal(t, float64(0), result["total_storage_used"])
	assert.Equal(t, float64(0), result["usage_percentage"])
}

// TestAPIEndpoints_Handle10kConcurrentRequests verifies that /api/* endpoints can
// handle 10,000 concurrent requests without errors or data races, alternating
// between endpoint1, endpoint2, and metrics.
func TestAPIEndpoints_Handle10kConcurrentRequests(t *testing.T) {
	const concurrency = 10_000
	app, ms, _ := setupApp(t, int64(concurrency), 1_000_000)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/endpoint1"},
		{http.MethodPost, "/api/endpoint2"},
		{http.MethodGet, "/api/metrics"},
	}

	var wg sync.WaitGroup
	wg.Add(concurrency)
	errs := make(chan string, concurrency)

	for i := 0; i < concurrency; i++ {
		ep := endpoints[i%len(endpoints)]
		go func(method, path string) {
			defer wg.Done()
			req := httptest.NewRequest(method, path, nil)
			resp, err := app.Test(req, -1)
			if err != nil {
				errs <- fmt.Sprintf("%s %s: %v", method, path, err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != fiber.StatusOK {
				errs <- fmt.Sprintf("%s %s: status %d", method, path, resp.StatusCode)
			}
		}(ep.method, ep.path)
	}

	wg.Wait()
	close(errs)

	var failures []string
	for e := range errs {
		failures = append(failures, e)
	}
	assert.Empty(t, failures, "concurrent request failures: %v", failures)

	// Only POST endpoints are tracked (/api/metrics is untracked).
	// With round-robin across 3 endpoints, metrics gets concurrency/3 requests.
	metricsRequests := int64(concurrency / 3)
	expectedTracked := int64(concurrency) - metricsRequests
	assert.Equal(t, expectedTracked, ms.GetTotalRequests())
}

// --- helpers ---

func parseJSON(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &result), "response body: %s", string(data))
	return result
}

func createMultipartFile(t *testing.T, fieldName, fileName string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile(fieldName, fileName)
	require.NoError(t, err)

	_, err = io.Copy(part, bytes.NewReader(content))
	require.NoError(t, err)

	ct := writer.FormDataContentType()
	require.NoError(t, writer.Close())
	return &buf, ct
}
