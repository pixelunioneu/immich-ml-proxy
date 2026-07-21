# ADR-0001: Route by single top-level task key, no fan-out/merge

## Status

Accepted (2026-07-21)

## Context

`immich-ml-gpu` was throwing CUDA OOM errors under load because all three
model pipelines (`clip`, `facial-recognition`, `ocr`) share one 1080 Ti's
11GB of VRAM per replica, with essentially no headroom once all three are
loaded. The plan was to split OCR onto a CPU-only backend via a proxy in
front of `immich-machine-learning`.

Two designs were considered for how the proxy should route requests:

1. **Fan-out + merge**: assume a single request might contain multiple task
   types at once (e.g. `{"clip": {...}, "ocr": {...}}`). The proxy would
   split the request by task key, forward each piece to the appropriate
   backend concurrently, and merge the JSON responses back into one body
   keyed by task, replicating whatever ordering/dependency semantics Immich
   expects.
2. **Single-key routing**: assume each request contains exactly one task
   type, and route the whole request, unmodified, to whichever backend
   handles that type.

## Decision

Single-key routing (design 2).

We checked `/Users/jochem/pu/immich`'s server source directly rather than
assuming. `server/src/repositories/machine-learning.repository.ts` defines
`MachineLearningRequest` as a **union** type
(`ClipVisualRequest | ClipTextualRequest | FacialRecognitionRequest |
OcrRequest`), not an intersection — a single call is structurally limited to
one task. Every job caller confirms this in practice:

- Smart Search job → `encodeImage()` / `encodeText()` → `{clip: {...}}` only
  (`server/src/services/smart-info.service.ts:111`,
  `server/src/services/search.service.ts:155`)
- Face Detection job → `detectFaces()` → `{facial-recognition: {...}}` only
  (`server/src/services/person.service.ts:317-319`)
- OCR job → `ocr()` → `{ocr: {...}}` only
  (`server/src/services/ocr.service.ts:56`)

The Python side's `PipelineRequest` schema
(`machine-learning/immich_ml/schemas.py:109`,
`dict[ModelTask, dict[ModelType, PipelineEntry]]`) is structurally *capable*
of holding multiple task keys in one call, but nothing in the current
TypeScript codebase ever constructs such a payload. Building fan-out/merge
machinery for a case that doesn't occur would be unused complexity, coupled
to Immich's internal request schema for no present benefit.

## Consequences

- The proxy is a simple, upstream-schema-agnostic router: peek the request
  body's top-level JSON keys, pick a backend, forward unmodified. No
  understanding of per-task dependency structure (e.g. face detection
  feeding face recognition) is needed, since that's all handled inside a
  single task's own request/response on one backend.
- This is coupled to an Immich behavior that could change in a future
  version. If Immich ever starts bundling multiple task types per request,
  this proxy would route the whole request to one backend based on
  whichever key it happens to check, silently dropping the other task's
  processing rather than erroring. There's no compile-time or schema-level
  guard against that regression — it would show up as: results missing for
  one task type, or an uptick in `immich_ml_proxy_route_fallback_total` /
  unexpected multi-key bodies in the request logs. Re-verify this assumption
  against `machine-learning.repository.ts` when upgrading Immich across a
  major version.
