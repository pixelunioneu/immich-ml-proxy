package proxy

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pixelunioneu/immich-ml-proxy/internal/router"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestServeHTTP_RoutesToOCRBackend(t *testing.T) {
	var hitOCR, hitDefault bool

	ocrStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitOCR = true
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"ocr":{}}` {
			t.Errorf("ocr backend got unexpected body: %s", body)
		}
		w.Header().Set("X-Test", "ocr-response")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ocr":"result"}`))
	}))
	defer ocrStub.Close()

	defaultStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitDefault = true
		w.WriteHeader(http.StatusOK)
	}))
	defer defaultStub.Close()

	h := New(Config{
		Router:            router.New(map[string]struct{}{"ocr": {}}),
		DefaultBackendURL: mustURL(t, defaultStub.URL),
		OCRBackendURL:     mustURL(t, ocrStub.URL),
		MaxBodyBytes:      1 << 20,
		RequestTimeout:    5 * time.Second,
		Logger:            testLogger(),
	})

	req := httptest.NewRequest(http.MethodPost, "/predict", strings.NewReader(`{"ocr":{}}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !hitOCR || hitDefault {
		t.Fatalf("expected request to hit OCR backend only; hitOCR=%v hitDefault=%v", hitOCR, hitDefault)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("X-Test") != "ocr-response" {
		t.Errorf("response headers not passed through: %v", rec.Header())
	}
	if body := rec.Body.String(); body != `{"ocr":"result"}` {
		t.Errorf("response body = %q, want %q", body, `{"ocr":"result"}`)
	}
}

func TestServeHTTP_RoutesToDefaultBackend(t *testing.T) {
	var hitOCR, hitDefault bool

	ocrStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitOCR = true
		w.WriteHeader(http.StatusOK)
	}))
	defer ocrStub.Close()

	defaultStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitDefault = true
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"clip":{}}` {
			t.Errorf("default backend got unexpected body: %s", body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"clip":"result"}`))
	}))
	defer defaultStub.Close()

	h := New(Config{
		Router:            router.New(map[string]struct{}{"ocr": {}}),
		DefaultBackendURL: mustURL(t, defaultStub.URL),
		OCRBackendURL:     mustURL(t, ocrStub.URL),
		MaxBodyBytes:      1 << 20,
		RequestTimeout:    5 * time.Second,
		Logger:            testLogger(),
	})

	req := httptest.NewRequest(http.MethodPost, "/predict", strings.NewReader(`{"clip":{}}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if hitOCR || !hitDefault {
		t.Fatalf("expected request to hit default backend only; hitOCR=%v hitDefault=%v", hitOCR, hitDefault)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestServeHTTP_SendsBasicAuthToBackend(t *testing.T) {
	var gotUser, gotPass string
	var gotOK bool

	defaultStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, gotOK = r.BasicAuth()
		w.WriteHeader(http.StatusOK)
	}))
	defer defaultStub.Close()

	h := New(Config{
		Router:                 router.New(map[string]struct{}{"ocr": {}}),
		DefaultBackendURL:      mustURL(t, defaultStub.URL),
		DefaultBackendUsername: "gpu-user",
		DefaultBackendPassword: "gpu-pass",
		OCRBackendURL:          mustURL(t, defaultStub.URL),
		MaxBodyBytes:           1 << 20,
		RequestTimeout:         5 * time.Second,
		Logger:                 testLogger(),
	})

	req := httptest.NewRequest(http.MethodPost, "/predict", strings.NewReader(`{"clip":{}}`))
	req.Header.Set("Authorization", "Bearer client-supplied-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !gotOK {
		t.Fatal("backend did not receive Basic Auth credentials")
	}
	if gotUser != "gpu-user" || gotPass != "gpu-pass" {
		t.Errorf("backend got user=%q pass=%q, want gpu-user/gpu-pass", gotUser, gotPass)
	}
}

func TestServeHTTP_NoBasicAuthConfigured(t *testing.T) {
	var gotAuthHeader string

	defaultStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthHeader = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer defaultStub.Close()

	h := New(Config{
		Router:            router.New(map[string]struct{}{"ocr": {}}),
		DefaultBackendURL: mustURL(t, defaultStub.URL),
		OCRBackendURL:     mustURL(t, defaultStub.URL),
		MaxBodyBytes:      1 << 20,
		RequestTimeout:    5 * time.Second,
		Logger:            testLogger(),
	})

	req := httptest.NewRequest(http.MethodPost, "/predict", strings.NewReader(`{"clip":{}}`))
	req.Header.Set("Authorization", "Bearer client-supplied-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if gotAuthHeader != "Bearer client-supplied-token" {
		t.Errorf("Authorization header = %q, want client's header to pass through unmodified", gotAuthHeader)
	}
}

func TestServeHTTP_BackendDown(t *testing.T) {
	// A server that's immediately closed simulates a connection failure.
	deadStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := deadStub.URL
	deadStub.Close()

	h := New(Config{
		Router:            router.New(map[string]struct{}{"ocr": {}}),
		DefaultBackendURL: mustURL(t, deadURL),
		OCRBackendURL:     mustURL(t, deadURL),
		MaxBodyBytes:      1 << 20,
		RequestTimeout:    2 * time.Second,
		Logger:            testLogger(),
	})

	req := httptest.NewRequest(http.MethodPost, "/predict", strings.NewReader(`{"clip":{}}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestServeHTTP_BodyTooLarge(t *testing.T) {
	defaultStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer defaultStub.Close()

	h := New(Config{
		Router:            router.New(map[string]struct{}{"ocr": {}}),
		DefaultBackendURL: mustURL(t, defaultStub.URL),
		OCRBackendURL:     mustURL(t, defaultStub.URL),
		MaxBodyBytes:      4, // tiny limit
		RequestTimeout:    2 * time.Second,
		Logger:            testLogger(),
	})

	req := httptest.NewRequest(http.MethodPost, "/predict", strings.NewReader(`{"clip":{}}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", rec.Code)
	}
}
