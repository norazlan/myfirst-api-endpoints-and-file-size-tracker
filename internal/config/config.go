package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config holds all application configuration values loaded from environment variables.
type Config struct {
	AppPort       string
	StorageLimit  int64
	RequestLimit  int64
	UploadDir     string
	DBPath        string
	FlushInterval int
}

// LoadConfig reads the .env file and returns a Config struct with parsed values.
// Falls back to sensible defaults if variables are not set.
func LoadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		// .env file is optional in production; log but don't fail
		_ = err
	}

	storageLimit, err := strconv.ParseInt(getEnv("STORAGE_LIMIT", "1073741824"), 10, 64)
	if err != nil {
		return nil, err
	}

	requestLimit, err := strconv.ParseInt(getEnv("REQUEST_LIMIT", "1000"), 10, 64)
	if err != nil {
		return nil, err
	}

	flushInterval, err := strconv.Atoi(getEnv("FLUSH_INTERVAL", "30"))
	if err != nil {
		return nil, err
	}

	return &Config{
		AppPort:       getEnv("APP_PORT", "3000"),
		StorageLimit:  storageLimit,
		RequestLimit:  requestLimit,
		UploadDir:     getEnv("UPLOAD_DIR", "./uploads"),
		DBPath:        getEnv("DB_PATH", "./metering.db"),
		FlushInterval: flushInterval,
	}, nil
}

// getEnv returns the environment variable value or a fallback default.
func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return fallback
}
