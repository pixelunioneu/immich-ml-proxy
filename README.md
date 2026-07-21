# immich-ml-proxy

A small reverse proxy in front of `immich-machine-learning` that routes each
`/predict` request to one of two backends based on its top-level task key:

- `ocr` → the OCR backend (intended to be a CPU-only `immich-ml` replica)
- anything else (`clip`, `facial-recognition`, ...) → the default backend
  (the GPU `immich-ml-gpu` replicas)

## Why

`immich-ml-gpu`'s two 1080 Tis (11GB VRAM each) run all three model
pipelines per replica. Once `clip` + `facial-recognition` + `ocr`'s ONNX
Runtime CUDA arenas are all resident, there's ~100MB of headroom left, and
OCR's recognition stage (which batches per detected text-line, with no
fixed-size arena) throws CUDA OOM errors under concurrent load. Immich never
sends more than one task type per request, so
routing by top-level JSON key lets OCR run on CPU — where its ~20MB of
weights are trivial to host — without touching `clip`/`facial-recognition`'s
GPU placement at all.

## Configuration

All configuration is via environment variables:

| Var | Required | Default | Description |
|---|---|---|---|
| `LISTEN_ADDR` | no | `:3003` | address the proxy listens on |
| `DEFAULT_BACKEND_URL` | yes | — | base URL of the GPU backend, e.g. `https://immich-ml-gpu.example.internal` |
| `OCR_BACKEND_URL` | yes | — | base URL of the OCR/CPU backend, e.g. `http://immich-ml.immich-ml.svc.cluster.local:3003` |
| `OCR_TASK_KEYS` | no | `ocr` | comma-separated top-level JSON keys routed to the OCR backend |
| `REQUEST_TIMEOUT` | no | `60s` | per-request upstream timeout |
| `MAX_BODY_BYTES` | no | `10485760` (10MiB) | cap on buffered request body size |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, or `error` |

Both backend URLs may use `http://` or `https://` independently — the proxy
doesn't care which scheme a given backend uses.

## Endpoints

- `POST /predict` (or any path) — proxied to the routed backend, unmodified.
- `GET /healthz` — liveness; always 200.
- `GET /readyz` — readiness; 200 once config has loaded (does not probe
  backends, to avoid readiness flapping during a backend rollout).
- `GET /metrics` — Prometheus exposition:
  - `immich_ml_proxy_requests_total{backend,reason}`
  - `immich_ml_proxy_route_fallback_total{reason}`
  - `immich_ml_proxy_upstream_errors_total{backend}`
  - `immich_ml_proxy_request_duration_seconds{backend}`

## Routing behavior

Routing fails open to the default (GPU) backend whenever the request can't
be confidently classified — non-`/predict` paths, an empty body, or a body
that doesn't decode as a JSON object. This means a request Immich would have
sent successfully always gets forwarded somewhere; it's never rejected
because the proxy couldn't parse it. Watch
`immich_ml_proxy_route_fallback_total` for how often that's happening.

## Development

```sh
go test ./... -race -cover
golangci-lint run
```

```sh
DEFAULT_BACKEND_URL=https://immich-ml-gpu.example.internal \
OCR_BACKEND_URL=http://immich-ml.immich-ml.svc.cluster.local:3003 \
go run ./cmd/proxy
```

## Repo layout

```
cmd/proxy/            entrypoint: config load, server start, graceful shutdown
internal/config/       env-var config loading + validation
internal/router/        routing decision: JSON top-level key -> backend
internal/proxy/          HTTP handler: buffer, route, forward, stream response
internal/metrics/        Prometheus metric definitions
deploy/kustomize/        Deployment/Service/ConfigMap for this proxy
```

Code lives under `internal/` rather than `pkg/` (unlike some sibling `pu-*`
Go services) — this is a standalone binary with no intended external
importers, and `internal/` is the stronger Go convention for that case.

## Deployment status

This repo builds and tests the proxy itself. Wiring it into the platform —
an ArgoCD `Application` deploying `deploy/kustomize/`, and repointing
tenant Immich config (`immich-charts/charts/immich-tenants/files/
immich-config-general.yaml`'s `machineLearning.urls`) at this proxy instead
of directly at `immich-ml-gpu` — is a follow-up, blocked on fixing the
existing CPU-only `immich-ml` release's pod scheduling (currently 0/1
available in the `immich-ml` namespace).
