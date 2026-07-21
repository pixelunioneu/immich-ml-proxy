// Package config loads and validates the proxy's runtime configuration from
// environment variables.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the proxy's runtime configuration.
type Config struct {
	ListenAddr        string
	DefaultBackendURL *url.URL
	OCRBackendURL     *url.URL
	OCRTaskKeys       map[string]struct{}
	RequestTimeout    time.Duration
	MaxBodyBytes      int64
	LogLevel          string
}

// Load reads configuration from the environment and validates it. It returns
// an error describing the first problem found rather than starting with a
// partially-invalid configuration.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:     getEnv("LISTEN_ADDR", ":3003"),
		RequestTimeout: 60 * time.Second,
		MaxBodyBytes:   10 * 1024 * 1024,
		LogLevel:       getEnv("LOG_LEVEL", "info"),
	}

	defaultBackend := os.Getenv("DEFAULT_BACKEND_URL")
	if defaultBackend == "" {
		return nil, fmt.Errorf("DEFAULT_BACKEND_URL is required")
	}
	defaultURL, err := parseBackendURL(defaultBackend)
	if err != nil {
		return nil, fmt.Errorf("DEFAULT_BACKEND_URL: %w", err)
	}
	cfg.DefaultBackendURL = defaultURL

	ocrBackend := os.Getenv("OCR_BACKEND_URL")
	if ocrBackend == "" {
		return nil, fmt.Errorf("OCR_BACKEND_URL is required")
	}
	ocrURL, err := parseBackendURL(ocrBackend)
	if err != nil {
		return nil, fmt.Errorf("OCR_BACKEND_URL: %w", err)
	}
	cfg.OCRBackendURL = ocrURL

	cfg.OCRTaskKeys = parseTaskKeys(getEnv("OCR_TASK_KEYS", "ocr"))
	if len(cfg.OCRTaskKeys) == 0 {
		return nil, fmt.Errorf("OCR_TASK_KEYS must contain at least one key")
	}

	if raw := os.Getenv("REQUEST_TIMEOUT"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("REQUEST_TIMEOUT: %w", err)
		}
		cfg.RequestTimeout = d
	}

	if raw := os.Getenv("MAX_BODY_BYTES"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("MAX_BODY_BYTES: must be a positive integer")
		}
		cfg.MaxBodyBytes = n
	}

	return cfg, nil
}

func parseBackendURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid URL %q: scheme must be http or https", raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q: missing host", raw)
	}
	return u, nil
}

func parseTaskKeys(raw string) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, k := range strings.Split(raw, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			keys[k] = struct{}{}
		}
	}
	return keys
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
