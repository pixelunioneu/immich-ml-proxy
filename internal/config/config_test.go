package config

import (
	"os"
	"testing"
	"time"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"LISTEN_ADDR", "DEFAULT_BACKEND_URL", "OCR_BACKEND_URL",
		"OCR_TASK_KEYS", "REQUEST_TIMEOUT", "MAX_BODY_BYTES", "LOG_LEVEL",
	} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
}

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "https://gpu.example.com")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com:3003")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.ListenAddr != ":3003" {
		t.Errorf("ListenAddr = %q, want :3003", cfg.ListenAddr)
	}
	if cfg.RequestTimeout != 60*time.Second {
		t.Errorf("RequestTimeout = %v, want 60s", cfg.RequestTimeout)
	}
	if cfg.MaxBodyBytes != 10*1024*1024 {
		t.Errorf("MaxBodyBytes = %d, want 10MiB", cfg.MaxBodyBytes)
	}
	if _, ok := cfg.OCRTaskKeys["ocr"]; !ok {
		t.Errorf("OCRTaskKeys = %v, want to contain \"ocr\"", cfg.OCRTaskKeys)
	}
	if cfg.DefaultBackendURL.String() != "https://gpu.example.com" {
		t.Errorf("DefaultBackendURL = %v", cfg.DefaultBackendURL)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	clearEnv(t)
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DEFAULT_BACKEND_URL and OCR_BACKEND_URL are unset")
	}

	t.Setenv("DEFAULT_BACKEND_URL", "https://gpu.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when OCR_BACKEND_URL is unset")
	}
}

func TestLoad_InvalidBackendURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "not-a-url")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for scheme-less DEFAULT_BACKEND_URL")
	}

	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "ftp://gpu.example.com")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for non-http(s) scheme")
	}
}

func TestLoad_CustomOCRTaskKeys(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "https://gpu.example.com")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com")
	t.Setenv("OCR_TASK_KEYS", "ocr, facial-recognition")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	for _, k := range []string{"ocr", "facial-recognition"} {
		if _, ok := cfg.OCRTaskKeys[k]; !ok {
			t.Errorf("OCRTaskKeys missing %q: %v", k, cfg.OCRTaskKeys)
		}
	}
}

func TestLoad_InvalidRequestTimeout(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "https://gpu.example.com")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com")
	t.Setenv("REQUEST_TIMEOUT", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid REQUEST_TIMEOUT")
	}
}

func TestLoad_InvalidMaxBodyBytes(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "https://gpu.example.com")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com")
	t.Setenv("MAX_BODY_BYTES", "-5")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for negative MAX_BODY_BYTES")
	}
}
