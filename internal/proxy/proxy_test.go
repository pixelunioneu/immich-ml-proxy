package proxy

import (
	"bytes"
	"io"
	"log/slog"
	"mime/multipart"
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

// multipartPredictRequest builds a request shaped like the real
// immich-machine-learning /predict call: multipart/form-data with an
// "entries" field (task-keyed JSON) and an "image" file part.
func multipartPredictRequest(t *testing.T, entries string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("entries", entries); err != nil {
		t.Fatalf("write entries field: %v", err)
	}
	fw, err := w.CreateFormFile("image", "photo.jpg")
	if err != nil {
		t.Fatalf("create image part: %v", err)
	}
	if _, err := fw.Write([]byte("fake-jpeg-bytes")); err != nil {
		t.Fatalf("write image part: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/predict", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
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

// TestServeHTTP_MultipartRoutesToOCRBackend covers the actual
// immich-machine-learning wire format: multipart/form-data with the
// routable task key inside the "entries" field, not the raw body.
func TestServeHTTP_MultipartRoutesToOCRBackend(t *testing.T) {
	var hitOCR, hitDefault bool
	var gotContentType string

	ocrStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitOCR = true
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
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

	req := multipartPredictRequest(t, `{"ocr":{"recognition":{"modelName":"PP-OCRv5_mobile"}}}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !hitOCR || hitDefault {
		t.Fatalf("expected multipart request to hit OCR backend only; hitOCR=%v hitDefault=%v", hitOCR, hitDefault)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.HasPrefix(gotContentType, "multipart/form-data") {
		t.Errorf("backend Content-Type = %q, want multipart/form-data (body forwarded unmodified)", gotContentType)
	}
}

func TestServeHTTP_MultipartRoutesToDefaultBackend(t *testing.T) {
	var hitOCR, hitDefault bool

	ocrStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitOCR = true
		w.WriteHeader(http.StatusOK)
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

	req := multipartPredictRequest(t, `{"clip":{"visual":{"modelName":"ViT-B-32"}}}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if hitOCR || !hitDefault {
		t.Fatalf("expected multipart request to hit default backend only; hitOCR=%v hitDefault=%v", hitOCR, hitDefault)
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRoutingPayload(t *testing.T) {
	t.Run("non-multipart body passes through unchanged", func(t *testing.T) {
		body := []byte(`{"clip":{}}`)
		got := routingPayload("application/json", body)
		if string(got) != string(body) {
			t.Errorf("got %q, want body passed through unchanged", got)
		}
	})

	t.Run("multipart extracts the entries field", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("entries", `{"ocr":{}}`)
		fw, _ := w.CreateFormFile("image", "photo.jpg")
		_, _ = fw.Write([]byte("fake-jpeg"))
		_ = w.Close()

		got := routingPayload(w.FormDataContentType(), buf.Bytes())
		if string(got) != `{"ocr":{}}` {
			t.Errorf("got %q, want entries field content", got)
		}
	})

	t.Run("multipart without an entries field returns nil", func(t *testing.T) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("text", "a search query")
		_ = w.Close()

		got := routingPayload(w.FormDataContentType(), buf.Bytes())
		if got != nil {
			t.Errorf("got %q, want nil", got)
		}
	})

	t.Run("malformed content type falls back to raw body", func(t *testing.T) {
		body := []byte(`{"clip":{}}`)
		got := routingPayload("not a content type;;;", body)
		if string(got) != string(body) {
			t.Errorf("got %q, want body passed through unchanged", got)
		}
	})

	t.Run("multipart with no boundary returns nil", func(t *testing.T) {
		got := routingPayload("multipart/form-data", []byte("whatever"))
		if got != nil {
			t.Errorf("got %q, want nil", got)
		}
	})
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
