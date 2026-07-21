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
		"DEFAULT_BACKEND_BASIC_AUTH_USERNAME", "DEFAULT_BACKEND_BASIC_AUTH_PASSWORD",
		"OCR_BACKEND_BASIC_AUTH_USERNAME", "OCR_BACKEND_BASIC_AUTH_PASSWORD",
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

func TestLoad_BasicAuth(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "https://gpu.example.com")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com")
	t.Setenv("DEFAULT_BACKEND_BASIC_AUTH_USERNAME", "gpu-user")
	t.Setenv("DEFAULT_BACKEND_BASIC_AUTH_PASSWORD", "gpu-pass")
	t.Setenv("OCR_BACKEND_BASIC_AUTH_USERNAME", "ocr-user")
	t.Setenv("OCR_BACKEND_BASIC_AUTH_PASSWORD", "ocr-pass")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DefaultBackendUsername != "gpu-user" || cfg.DefaultBackendPassword != "gpu-pass" {
		t.Errorf("default backend auth = %q/%q, want gpu-user/gpu-pass", cfg.DefaultBackendUsername, cfg.DefaultBackendPassword)
	}
	if cfg.OCRBackendUsername != "ocr-user" || cfg.OCRBackendPassword != "ocr-pass" {
		t.Errorf("ocr backend auth = %q/%q, want ocr-user/ocr-pass", cfg.OCRBackendUsername, cfg.OCRBackendPassword)
	}
}

func TestLoad_BasicAuth_Unset(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "https://gpu.example.com")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.DefaultBackendUsername != "" || cfg.DefaultBackendPassword != "" {
		t.Errorf("expected no default backend auth, got %q/%q", cfg.DefaultBackendUsername, cfg.DefaultBackendPassword)
	}
	if cfg.OCRBackendUsername != "" || cfg.OCRBackendPassword != "" {
		t.Errorf("expected no ocr backend auth, got %q/%q", cfg.OCRBackendUsername, cfg.OCRBackendPassword)
	}
}

func TestLoad_BasicAuth_OnlyUsername(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "https://gpu.example.com")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com")
	t.Setenv("DEFAULT_BACKEND_BASIC_AUTH_USERNAME", "gpu-user")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DEFAULT_BACKEND_BASIC_AUTH_USERNAME is set without the password")
	}
}

func TestLoad_BasicAuth_OnlyPassword(t *testing.T) {
	clearEnv(t)
	t.Setenv("DEFAULT_BACKEND_URL", "https://gpu.example.com")
	t.Setenv("OCR_BACKEND_URL", "http://cpu.example.com")
	t.Setenv("OCR_BACKEND_BASIC_AUTH_PASSWORD", "ocr-pass")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when OCR_BACKEND_BASIC_AUTH_PASSWORD is set without the username")
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
