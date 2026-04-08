package handlers

import (
	"errors"

	"metering-api/internal/services"

	"github.com/gofiber/fiber/v2"
)

// MeteringHandler holds the dependencies for API metering endpoints.
type MeteringHandler struct {
	MeteringService *services.MeteringService
}

// NewMeteringHandler creates a new MeteringHandler.
func NewMeteringHandler(ms *services.MeteringService) *MeteringHandler {
	return &MeteringHandler{MeteringService: ms}
}

// trackRequest increments the request counter for the current path.
// Returns true if the request was tracked successfully, false if a limit
// error response has already been sent to the client.
func (h *MeteringHandler) trackRequest(c *fiber.Ctx) bool {
	// Copy the path string to avoid fasthttp buffer reuse issues.
	path := string([]byte(c.Path()))
	if err := h.MeteringService.IncrementEndpoint(path); err != nil {
		if errors.Is(err, services.ErrRequestLimitExceeded) {
			_ = c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "request limit exceeded",
			})
		} else {
			_ = c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "internal server error",
			})
		}
		return false
	}
	return true
}

// HandleEndpoint1 is a sample tracked API endpoint.
// POST /api/endpoint1
func (h *MeteringHandler) HandleEndpoint1(c *fiber.Ctx) error {
	if !h.trackRequest(c) {
		return nil
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "request to endpoint1 processed successfully",
	})
}

// HandleEndpoint2 is a sample tracked API endpoint.
// POST /api/endpoint2
func (h *MeteringHandler) HandleEndpoint2(c *fiber.Ctx) error {
	if !h.trackRequest(c) {
		return nil
	}
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"message": "request to endpoint2 processed successfully",
	})
}

// GetMetrics returns the request counts for all tracked endpoints.
// GET /api/metrics
func (h *MeteringHandler) GetMetrics(c *fiber.Ctx) error {
	metrics := h.MeteringService.GetMetrics()
	total := h.MeteringService.GetTotalRequests()
	limit := h.MeteringService.GetRequestLimit()

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"endpoints":      metrics,
		"total_requests": total,
		"request_limit":  limit,
	})
}

// StorageHandler holds the dependencies for storage metering endpoints.
type StorageHandler struct {
	StorageService *services.StorageService
}

// NewStorageHandler creates a new StorageHandler.
func NewStorageHandler(ss *services.StorageService) *StorageHandler {
	return &StorageHandler{
		StorageService: ss,
	}
}

// UploadFile handles file uploads and tracks storage usage.
// POST /upload — this endpoint is not metered (excluded from request counting).
func (h *StorageHandler) UploadFile(c *fiber.Ctx) error {
	fh, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "file is required",
		})
	}

	if fh.Size == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "uploaded file is empty",
		})
	}

	// Save file to disk
	savedName, err := h.StorageService.SaveFile(fh)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to save file",
		})
	}

	// Track storage usage (check limit)
	if err := h.StorageService.TrackUpload(savedName, fh.Size); err != nil {
		if errors.Is(err, services.ErrStorageLimitExceeded) {
			return c.Status(fiber.StatusRequestEntityTooLarge).JSON(fiber.Map{
				"error": "storage limit exceeded",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "failed to track upload",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"message":  "file uploaded successfully",
		"filename": savedName,
		"size":     fh.Size,
	})
}

// GetStorage returns the current storage usage information.
// GET /storage
func (h *StorageHandler) GetStorage(c *fiber.Ctx) error {
	totalUsed := h.StorageService.GetTotalStorage()
	limit := h.StorageService.GetStorageLimit()

	var usagePercent float64
	if limit > 0 {
		usagePercent = float64(totalUsed) / float64(limit) * 100
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"total_storage_used": totalUsed,
		"storage_limit":      limit,
		"usage_percentage":   usagePercent,
	})
}
