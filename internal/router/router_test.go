package router

import "testing"

func defaultRouter() *Router {
	return New(map[string]struct{}{"ocr": {}})
}

func TestRoute(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		body     string
		want     Backend
		wantReas Reason
	}{
		{
			name: "clip only routes to default",
			path: "/predict",
			body: `{"clip": {"visual": {}}}`,
			want: Default, wantReas: ReasonNoOCRKey,
		},
		{
			name: "facial-recognition only routes to default",
			path: "/predict",
			body: `{"facial-recognition": {"detection": {}, "recognition": {}}}`,
			want: Default, wantReas: ReasonNoOCRKey,
		},
		{
			name: "ocr only routes to ocr backend",
			path: "/predict",
			body: `{"ocr": {"detection": {}, "recognition": {}}}`,
			want: OCR, wantReas: ReasonOCRKey,
		},
		{
			name: "ocr among other keys still routes to ocr backend",
			path: "/predict",
			body: `{"clip": {}, "ocr": {}}`,
			want: OCR, wantReas: ReasonOCRKey,
		},
		{
			name: "unknown key routes to default",
			path: "/predict",
			body: `{"some-future-task": {}}`,
			want: Default, wantReas: ReasonNoOCRKey,
		},
		{
			name: "malformed json falls back to default",
			path: "/predict",
			body: `{not valid json`,
			want: Default, wantReas: ReasonDecodeError,
		},
		{
			name: "json array (not object) falls back to default",
			path: "/predict",
			body: `[1, 2, 3]`,
			want: Default, wantReas: ReasonDecodeError,
		},
		{
			name: "empty body falls back to default",
			path: "/predict",
			body: "",
			want: Default, wantReas: ReasonEmptyBody,
		},
		{
			name: "non-predict path falls back to default",
			path: "/ping",
			body: `{"ocr": {}}`,
			want: Default, wantReas: ReasonNotPredict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := defaultRouter().Route(tt.path, []byte(tt.body))
			if got.Backend != tt.want {
				t.Errorf("Backend = %v, want %v", got.Backend, tt.want)
			}
			if got.Reason != tt.wantReas {
				t.Errorf("Reason = %v, want %v", got.Reason, tt.wantReas)
			}
		})
	}
}

func TestRoute_ConfigurableTaskKeys(t *testing.T) {
	r := New(map[string]struct{}{"ocr": {}, "facial-recognition": {}})

	got := r.Route("/predict", []byte(`{"facial-recognition": {}}`))
	if got.Backend != OCR {
		t.Errorf("expected facial-recognition to route to OCR backend when configured, got %v", got.Backend)
	}

	got = r.Route("/predict", []byte(`{"clip": {}}`))
	if got.Backend != Default {
		t.Errorf("expected clip to still route to Default backend, got %v", got.Backend)
	}
}
