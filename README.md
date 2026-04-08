# Metering API

A metering system for API request tracking and server storage monitoring, built with Go and the Fiber framework.

## Features

- **API Request Metering** — Tracks the number of requests made to each endpoint with a configurable global request limit (default: 1,000 requests).
- **Storage Metering** — Tracks total storage consumed by uploaded files with a configurable limit (default: 1 GB).
- **Concurrency-Safe** — Uses `sync.RWMutex` to protect shared state, safe for 10k+ concurrent requests.
- **Persistent Counters** — Request counts and storage records are persisted in SQLite via GORM; counters survive server restarts.
- **Graceful Shutdown** — Flushes in-memory counters to the database on SIGINT/SIGTERM.

## Architecture

```
cmd/server/main.go          Entry point — wires config, DB, services, routes, graceful shutdown
internal/
  config/config.go           Loads .env variables into a Config struct
  database/database.go       SQLite connection and GORM auto-migration
  models/models.go           EndpointMetric and StorageRecord GORM models
  services/
    metering_service.go      In-memory request counters (RWMutex + map) with DB flush
    storage_service.go       Storage tracking with DB-backed total and file persistence
  handlers/handlers.go       HTTP handlers with inline request tracking and limit enforcement
```

### Data Flow

1. **API requests** to tracked endpoints (`/api/endpoint1`, `/api/endpoint2`) call a `trackRequest` helper inside each handler, which increments a per-endpoint counter in memory. If the global total reaches the configured limit, the handler returns `429 Too Many Requests`. The `/api/metrics`, `/upload`, and `/storage` endpoints do not call `trackRequest`, so viewing metrics, uploading files, or checking storage does not consume the request quota. This handler-level approach ensures only requests matching the correct HTTP method are tracked (e.g., `GET /api/endpoint1` returns 405 and is never counted).
2. **File uploads** (`POST /upload`) are handled by the storage handler. The file is saved to disk with a UUID-prefixed name, and a `StorageRecord` is written to SQLite. Uploads are **not** counted against the global request limit.
3. **Periodic flush** — A background goroutine writes in-memory request counters to SQLite every N seconds (configurable via `FLUSH_INTERVAL`). A flush also runs on graceful shutdown.

### Key Design Decisions

| Decision                          | Rationale                                                                                                        |
| :-------------------------------- | :--------------------------------------------------------------------------------------------------------------- |
| **sync.RWMutex** over sync/atomic | Need to protect a `map[string]int64`; RWMutex allows concurrent reads for `GET /metrics`.                        |
| **In-memory counters + DB flush** | Hot path (increment) is lock-only, no I/O. Periodic flush avoids write bottleneck while ensuring durability.     |
| **Handler-level counting**        | Ensures only requests matching the correct HTTP method are tracked; avoids counting 405 Method Not Allowed hits. |
| **Global request limit**          | The spec says "1k requests" — interpreted as a global lifetime cap across all endpoints.                         |
| **UUID-prefixed filenames**       | Prevents filename collisions when multiple users upload files with the same name.                                |
| **SQLite via GORM**               | Zero-configuration embedded database; no external services required.                                             |

## Prerequisites

- Go 1.21+ (uses `go.mod`)
- GCC (required by `go-sqlite3` CGO driver)

## Setup

1. **Clone the repository:**

   ```bash
   git clone https://github.com/norazlan/myfirst-api-endpoints-and-file-size-tracker
   cd myfirst-api-endpoints-and-file-size-tracker
   ```

2. **Configure environment variables:**

   ```bash
   cp .env.example .env
   # Edit .env as needed
   ```

3. **Install dependencies:**
   ```bash
   go mod download
   ```

## Configuration

All settings are loaded from the `.env` file:

| Variable         | Default         | Description                                |
| :--------------- | :-------------- | :----------------------------------------- |
| `APP_PORT`       | `3000`          | HTTP server port                           |
| `STORAGE_LIMIT`  | `1073741824`    | Max total storage in bytes (1 GB)          |
| `REQUEST_LIMIT`  | `1000`          | Max total API requests before access stops |
| `UPLOAD_DIR`     | `./uploads`     | Directory for uploaded files               |
| `DB_PATH`        | `./metering.db` | SQLite database file path                  |
| `FLUSH_INTERVAL` | `30`            | Seconds between counter flushes to DB      |

## Running

```bash
# Run directly
make run

# Or build and execute the binary
make build
./metering-api

# Clean build artifacts and database
make clean
```

The server starts on `http://localhost:3000` (or the port configured in `.env`).

## API Endpoints

### API Metering

| Method | Path             | Description                                      |
| :----- | :--------------- | :----------------------------------------------- |
| `POST` | `/api/endpoint1` | Sample tracked endpoint                          |
| `POST` | `/api/endpoint2` | Sample tracked endpoint                          |
| `GET`  | `/api/metrics`   | Retrieve per-endpoint request counts (untracked) |

### Storage Metering

| Method | Path       | Description                                              |
| :----- | :--------- | :------------------------------------------------------- |
| `POST` | `/upload`  | Upload a file (multipart form, field: `file`, untracked) |
| `GET`  | `/storage` | Retrieve total storage usage                             |

### Example Usage

```bash
# Track a request to endpoint1
curl -X POST http://localhost:3000/api/endpoint1

# Track a request to endpoint2
curl -X POST http://localhost:3000/api/endpoint2

# View metrics (this request is not counted)
curl http://localhost:3000/api/metrics

# Upload a file
curl -F "file=@testfile.txt" http://localhost:3000/upload

# Check storage usage
curl http://localhost:3000/storage
```

### Response Examples

**GET /api/metrics**

```json
{
  "endpoints": {
    "/api/endpoint1": 5,
    "/api/endpoint2": 3
  },
  "total_requests": 8,
  "request_limit": 1000
}
```

**POST /upload** (201 Created)

```json
{
  "message": "file uploaded successfully",
  "filename": "a1b2c3d4-e5f6-7890-abcd-ef1234567890_testfile.txt",
  "size": 1024
}
```

**GET /storage**

```json
{
  "total_storage_used": 1024,
  "storage_limit": 1073741824,
  "usage_percentage": 0.0000954
}
```

### Error Responses

| Status                  | Condition                         |
| :---------------------- | :-------------------------------- |
| `400 Bad Request`       | Missing or empty file in upload   |
| `413 Payload Too Large` | Upload would exceed storage limit |
| `429 Too Many Requests` | Global request limit reached      |

## Testing

```bash
# Run all tests with race detection
make test

# Run a specific test
go test ./internal/services -v -run TestMeteringService_ConcurrentIncrements
```

### Test Coverage

- **Service tests** (12 tests) — Concurrent increments, limit enforcement, DB persistence, hydration on startup, idempotent flush.
- **Handler tests** (10 tests) — HTTP-level tests for all endpoints covering success, bad request, storage limit exceeded, request limit exceeded, and 10k concurrent request scenarios.
- All tests use in-memory SQLite databases and temporary directories — no side effects.
- Tests run with `-race` flag to detect data races.

## Known Limitations

- **Request limit is a global lifetime cap** — once reached, the server rejects all further API requests until restarted or the database is cleared. A time-window-based rate limiter would be more practical for production.
- **No authentication/authorization** — all endpoints are publicly accessible.
- **Single-node only** — in-memory counters are not shared across multiple server instances. A distributed counter (e.g., Redis) would be needed for horizontal scaling.
- **File cleanup** — if `TrackUpload` fails after `SaveFile` succeeds, the file remains on disk without a DB record. A cleanup mechanism or transactional approach would improve this.
- **No file download endpoint** — files are uploaded and tracked but cannot be retrieved via the API.

## Areas for Improvement

- Store uploaded files in external object storage (e.g., Amazon S3) instead of local disk — local storage does not scale horizontally, is lost when the container or server is replaced, and lacks built-in redundancy; S3 provides durable, highly available storage decoupled from the application server.
- Add per-endpoint and per-time-window rate limiting.
- Add authentication middleware (JWT, API keys).
- Replace SQLite with PostgreSQL for production workloads.
- Add a `/reset` admin endpoint to clear counters.
- Implement file download and deletion endpoints.
- Add structured logging (e.g., `zerolog`).
- Add Prometheus metrics export.
- Containerize with a Dockerfile.
