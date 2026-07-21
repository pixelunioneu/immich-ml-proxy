# CONTEXT

Single-context domain doc for `immich-ml-proxy`. This file holds the shared
vocabulary and the facts about the upstream system that the code relies on.

## What this is

A reverse proxy in front of `immich-machine-learning` (`immich-ml`) that
routes each request to one of two backend deployments based on which model
pipeline the request is for, so OCR can run on CPU while `clip` and
`facial-recognition` stay on GPU. See the README for configuration and
endpoints.

## Domain language

- **Task** — one of `clip`, `facial-recognition`, `ocr`: the top-level JSON
  key in a `/predict` request body, naming which immich-ml pipeline should
  handle it.
- **Backend** — one of two upstream `immich-ml` deployments this proxy
  forwards to: the **default** backend (GPU, runs all pipelines) and the
  **OCR** backend (intended to be CPU-only, runs only what's routed to it).
- **Routing decision** — the proxy's choice of backend for a request, made
  by inspecting the request body's top-level key(s) against the configured
  `OCR_TASK_KEYS` set. A decision is a **fallback** when it couldn't be
  confidently derived (bad path, empty/malformed body) and defaulted to the
  GPU backend instead.

## Upstream facts this proxy depends on

- **Immich never mixes task types in one request** (verified in
  `immich`'s server source, 2026-07-21): `MachineLearningRequest` is a union
  type, not an intersection, and every job caller (`smart-info.service.ts`,
  `person.service.ts`, `ocr.service.ts`) POSTs exactly one top-level key per
  call. This is the load-bearing assumption behind routing by top-level key
  with no fan-out/merge. If a future Immich version starts
  bundling multiple tasks per request, this proxy's routing would silently
  pick one backend and drop the other task's work; watch
  `immich_ml_proxy_route_fallback_total` and the request logs for early
  signs of that (e.g. a body with more than one recognized key).
- **immich-machine-learning falls back to CPU automatically.** Its ONNX
  Runtime execution provider list is
  `['CUDAExecutionProvider', 'CPUExecutionProvider']` in descending
  preference — a replica with no GPU attached runs on CPU with zero code
  changes. This is why the OCR backend can just be a normal `immich-ml`
  chart release without a GPU node selector, rather than a custom build.
- **OCR's VRAM pressure is a runtime/batching problem, not a model-size
  problem.** PP-OCRv5_mobile is ~20MB on disk (already the smaller of
  Immich's two supported variants); the OOMs come from the recognition
  stage's per-detected-text-line batching, which has no fixed-size CUDA
  arena the way `clip`'s fixed 224x224 input does.
