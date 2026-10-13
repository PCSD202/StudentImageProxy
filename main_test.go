package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestImageRequests(t *testing.T) {
	imageDir := t.TempDir()
	for name, contents := range map[string]string{
		"1114892.JPG":       "uppercase",
		"222.jpg":           "lowercase",
		"333.JPG":           "upper choice",
		"333.jpg":           "lower choice",
		"student-A_2.JPG":   "named ID",
		"name.with.dot.jpg": "dotted ID",
	} {
		if err := os.WriteFile(filepath.Join(imageDir, name), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(newHandler(imageDir, "secret"))
	defer server.Close()

	tests := []struct {
		path, key, method, rangeHeader string
		wantStatus                     int
		wantBody                       string
	}{
		{path: "/1114892", key: "secret", wantStatus: 200, wantBody: "uppercase"},
		{path: "/1114892.JPG", key: "secret", wantStatus: 200, wantBody: "uppercase"},
		{path: "/1114892.jpg", key: "secret", wantStatus: 200, wantBody: "uppercase"},
		{path: "/222", key: "secret", wantStatus: 200, wantBody: "lowercase"},
		{path: "/222.JPG", key: "secret", wantStatus: 200, wantBody: "lowercase"},
		{path: "/333", key: "secret", wantStatus: 200, wantBody: "upper choice"},
		{path: "/333.jpg", key: "secret", wantStatus: 200, wantBody: "lower choice"},
		{path: "/333.JPG", key: "secret", wantStatus: 200, wantBody: "upper choice"},
		{path: "/student-A_2", key: "secret", wantStatus: 200, wantBody: "named ID"},
		{path: "/name.with.dot", key: "secret", wantStatus: 200, wantBody: "dotted ID"},
		{path: "/999", key: "secret", wantStatus: 404},
		{path: "/999.jpg", key: "secret", wantStatus: 404},
		{path: "/1114892", wantStatus: 401},
		{path: "/1114892", key: "wrong", wantStatus: 401},
		{path: "/1114892", key: "secret", method: http.MethodPost, wantStatus: 405},
		{path: "/1114892", key: "secret", method: http.MethodHead, wantStatus: 200},
		{path: "/1114892", key: "secret", rangeHeader: "bytes=0-4", wantStatus: 206, wantBody: "upper"},
		{path: "/../1114892", key: "secret", wantStatus: 404},
		{path: "/1114892.png", key: "secret", wantStatus: 404},
		{path: "/123/1114892", key: "secret", wantStatus: 404},
		{path: "/..%5C1114892", key: "secret", wantStatus: 404},
		{path: "/health", wantStatus: 200, wantBody: "ok\n"},
	}
	for _, tt := range tests {
		t.Run(tt.method+tt.path+tt.key+tt.rangeHeader, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			req, err := http.NewRequest(method, server.URL+tt.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if tt.key != "" {
				req.Header.Set("X-API-Key", tt.key)
			}
			if tt.rangeHeader != "" {
				req.Header.Set("Range", tt.rangeHeader)
			}
			resp, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantStatus || (tt.wantBody != "" && string(body) != tt.wantBody) {
				t.Fatalf("got status %d, body %q; want status %d, body %q", resp.StatusCode, body, tt.wantStatus, tt.wantBody)
			}
			if tt.method == http.MethodHead && len(body) != 0 {
				t.Fatalf("HEAD returned a body: %q", body)
			}
			if tt.wantStatus == 200 && tt.path != "/health" && resp.Header.Get("Content-Type") != "image/jpeg" {
				t.Fatalf("unexpected content type: %q", resp.Header.Get("Content-Type"))
			}
		})
	}
}
