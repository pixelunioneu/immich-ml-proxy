// Package proxy implements the HTTP handler that inspects an
// immich-machine-learning request, routes it to the appropriate backend via
// internal/router, and forwards it unmodified.
package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pixelunioneu/immich-ml-proxy/internal/metrics"
	"github.com/pixelunioneu/immich-ml-proxy/internal/router"
)

// hopByHopHeaders lists headers that are connection-specific per RFC 7230
// §6.1 and must not be forwarded to (or from) the upstream.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// backendAuth holds optional HTTP Basic Auth credentials for a backend. A
// zero value means no auth is sent.
type backendAuth struct {
	username string
	password string
}

func (a backendAuth) configured() bool {
	return a.username != "" || a.password != ""
}

// Handler proxies requests to one of two backends based on routing.Route.
type Handler struct {
	router         *router.Router
	defaultBackend *url.URL
	defaultAuth    backendAuth
	ocrBackend     *url.URL
	ocrAuth        backendAuth
	maxBodyBytes   int64
	defaultClient  *http.Client
	ocrClient      *http.Client
	logger         *slog.Logger
}

// Config bundles the dependencies a Handler needs.
type Config struct {
	Router                 *router.Router
	DefaultBackendURL      *url.URL
	DefaultBackendUsername string
	DefaultBackendPassword string
	OCRBackendURL          *url.URL
	OCRBackendUsername     string
	OCRBackendPassword     string
	MaxBodyBytes           int64
	RequestTimeout         time.Duration
	Logger                 *slog.Logger
}

// New builds a Handler ready to serve requests.
func New(cfg Config) *Handler {
	newClient := func() *http.Client {
		return &http.Client{
			Timeout: cfg.RequestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		}
	}
	return &Handler{
		router:         cfg.Router,
		defaultBackend: cfg.DefaultBackendURL,
		defaultAuth:    backendAuth{username: cfg.DefaultBackendUsername, password: cfg.DefaultBackendPassword},
		ocrBackend:     cfg.OCRBackendURL,
		ocrAuth:        backendAuth{username: cfg.OCRBackendUsername, password: cfg.OCRBackendPassword},
		maxBodyBytes:   cfg.MaxBodyBytes,
		defaultClient:  newClient(),
		ocrClient:      newClient(),
		logger:         cfg.Logger,
	}
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	body, err := io.ReadAll(io.LimitReader(r.Body, h.maxBodyBytes+1))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadGateway)
		return
	}
	if int64(len(body)) > h.maxBodyBytes {
		http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
		return
	}

	decision := h.router.Route(r.URL.Path, routingPayload(r.Header.Get("Content-Type"), body))

	backendName := "default"
	backendURL := h.defaultBackend
	auth := h.defaultAuth
	client := h.defaultClient
	if decision.Backend == router.OCR {
		backendName = "ocr"
		backendURL = h.ocrBackend
		auth = h.ocrAuth
		client = h.ocrClient
	}

	metrics.RequestsTotal.WithLabelValues(backendName, string(decision.Reason)).Inc()
	if decision.Fallback {
		metrics.RouteFallbackTotal.WithLabelValues(string(decision.Reason)).Inc()
	}

	status, err := h.forward(r, w, client, backendURL, auth, body)
	duration := time.Since(start)
	metrics.RequestDuration.WithLabelValues(backendName).Observe(duration.Seconds())

	logAttrs := []any{
		slog.String("path", r.URL.Path),
		slog.String("backend", backendName),
		slog.String("reason", string(decision.Reason)),
		slog.Bool("fallback", decision.Fallback),
		slog.Duration("duration", duration),
	}
	if err != nil {
		metrics.UpstreamErrorsTotal.WithLabelValues(backendName).Inc()
		h.logger.Error("upstream request failed", append(logAttrs, slog.Any("error", err))...)
		return
	}
	logAttrs = append(logAttrs, slog.Int("status", status))
	h.logger.Info("proxied request", logAttrs...)
}

// routingPayload extracts the bytes the router should inspect to make a
// routing decision. immich-machine-learning's /predict takes
// multipart/form-data (an "entries" field holding the task-keyed JSON,
// plus separate image/text parts) rather than a raw JSON body, so the
// whole request body is never valid JSON on its own. For a multipart
// request, this pulls out just the "entries" part; anything else
// (missing part, bad boundary, non-multipart body) returns nil, which
// router.Route reports as an empty-body fallback.
func routingPayload(contentType string, body []byte) []byte {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return body
	}
	boundary, ok := params["boundary"]
	if !ok {
		return nil
	}
	mr := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := mr.NextPart()
		if err != nil {
			return nil
		}
		if part.FormName() == "entries" {
			data, err := io.ReadAll(part)
			if err != nil {
				return nil
			}
			return data
		}
	}
}

// forward sends body to backendURL+r.URL.Path(+query), streams the response
// back to w, and returns the upstream status code. If auth is configured,
// it sets the backend's Basic Auth credentials, overriding any
// Authorization header the original client sent. On failure it writes a
// 502 to w itself and returns the error.
func (h *Handler) forward(r *http.Request, w http.ResponseWriter, client *http.Client, backendURL *url.URL, auth backendAuth, body []byte) (int, error) {
	target := *backendURL
	target.Path = singleJoiningSlash(backendURL.Path, r.URL.Path)
	target.RawQuery = r.URL.RawQuery

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	outReq, err := http.NewRequestWithContext(ctx, r.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		http.Error(w, `{"error":"failed to build upstream request"}`, http.StatusBadGateway)
		return 0, err
	}
	copyHeaders(outReq.Header, r.Header)
	outReq.Host = backendURL.Host
	if auth.configured() {
		outReq.SetBasicAuth(auth.username, auth.password)
	}

	resp, err := client.Do(outReq)
	if err != nil {
		http.Error(w, `{"error":"upstream request failed"}`, http.StatusBadGateway)
		return 0, err
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil && !errors.Is(err, context.Canceled) {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func copyHeaders(dst, src http.Header) {
	for k, values := range src {
		if isHopByHop(k) {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func isHopByHop(header string) bool {
	for _, h := range hopByHopHeaders {
		if http.CanonicalHeaderKey(header) == http.CanonicalHeaderKey(h) {
			return true
		}
	}
	return false
}

func singleJoiningSlash(a, b string) string {
	aslash := len(a) > 0 && a[len(a)-1] == '/'
	bslash := len(b) > 0 && b[0] == '/'
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	default:
		return a + b
	}
}

// Healthz always reports healthy - it does not depend on backend reachability.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// Readyz reports ready once the handler has been constructed with valid
// config. It deliberately does not probe the backends, to avoid readiness
// flapping when a backend is mid-rollout.
func Readyz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}
