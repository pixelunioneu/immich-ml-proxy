// Package router decides which backend an immich-machine-learning request
// should be forwarded to, based on the top-level task key(s) present in the
// request body.
//
// Immich's server never mixes task types within a single /predict request —
// MachineLearningRequest is a union type and every job caller (smart search,
// face detection, OCR) sends exactly one top-level key. See docs/adr/0001.
// If that assumption ever stops holding, a request could legitimately need
// both backends; this router always picks one, so watch route_fallback_total
// and the "ocr"+"clip" fallback log lines for early warning of a mix.
package router

import (
	"encoding/json"
)

// Backend identifies which upstream a request should be sent to.
type Backend int

const (
	// Default is the general-purpose (GPU) backend.
	Default Backend = iota
	// OCR is the backend dedicated to OCR-tagged tasks.
	OCR
)

// Reason explains why a routing decision was made, for logging/metrics.
type Reason string

const (
	ReasonOCRKey      Reason = "ocr_key"
	ReasonNoOCRKey    Reason = "no_ocr_key"
	ReasonDecodeError Reason = "decode_error"
	ReasonEmptyBody   Reason = "empty_body"
	ReasonNotPredict  Reason = "not_predict_path"
)

// Decision is the outcome of a routing decision.
type Decision struct {
	Backend  Backend
	Reason   Reason
	Fallback bool // true if this decision fell back to Default rather than being confidently derived
}

// Router decides, for a given request path and body, which backend to use.
type Router struct {
	ocrTaskKeys map[string]struct{}
}

// New builds a Router that routes requests containing any of ocrTaskKeys as
// a top-level JSON key to the OCR backend.
func New(ocrTaskKeys map[string]struct{}) *Router {
	return &Router{ocrTaskKeys: ocrTaskKeys}
}

// Route decides which backend should receive a request for the given path
// with the given (already-buffered) body.
//
// Any ambiguity - a non-/predict path, an empty body, or a body that doesn't
// decode as a JSON object - fails open to the Default backend rather than
// rejecting the request, since immich-machine-learning would have accepted
// it directly.
func (r *Router) Route(path string, body []byte) Decision {
	if path != "/predict" {
		return Decision{Backend: Default, Reason: ReasonNotPredict, Fallback: true}
	}

	if len(body) == 0 {
		return Decision{Backend: Default, Reason: ReasonEmptyBody, Fallback: true}
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return Decision{Backend: Default, Reason: ReasonDecodeError, Fallback: true}
	}

	for key := range payload {
		if _, ok := r.ocrTaskKeys[key]; ok {
			return Decision{Backend: OCR, Reason: ReasonOCRKey}
		}
	}

	return Decision{Backend: Default, Reason: ReasonNoOCRKey}
}
