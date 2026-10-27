package http

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestPreviewWorkerAssetTypes(t *testing.T) {
	for _, test := range []struct{ path, mime string }{
		{"assets/preview.mjs", "text/javascript; charset=utf-8"},
		{"assets/decoder.wasm", "application/wasm"},
	} {
		t.Run(test.path, func(t *testing.T) {
			server := Server{Assets: fstest.MapFS{test.path: &fstest.MapFile{Data: []byte("fixture")}}}
			recorder := httptest.NewRecorder()
			server.handleSPA(recorder, httptest.NewRequest("GET", "/"+test.path, nil))
			if recorder.Code != 200 || recorder.Header().Get("Content-Type") != test.mime || recorder.Body.String() != "fixture" {
				t.Fatalf("asset response: status=%d mime=%q body=%q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
			}
		})
	}
}
