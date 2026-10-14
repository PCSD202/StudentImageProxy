package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func signedTestHandler(t *testing.T, key, baseURL string) (http.Handler, *urlSigner) {
	t.Helper()
	dir := t.TempDir()
	for _, id := range []string{"student-A", "student B+#?"} {
		if err := os.WriteFile(filepath.Join(dir, id+".JPG"), []byte("photo bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	images, err := newImageStore(dir, config{cacheBytes: 1 << 20, cacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	signer := newSigner(key, baseURL)
	signer.now = func() time.Time { return time.Unix(1800000000, 0) }
	return imageHandler(images, signer), signer
}

func signedRequest(handler http.Handler, method, target, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	if key != "" {
		r.Header.Set("X-API-Key", key)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func issueTestURL(t *testing.T, handler http.Handler, id string) string {
	t.Helper()
	w := signedRequest(handler, http.MethodPost, "/presign?id="+url.QueryEscape(id), "secret")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	var response struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.ExpiresAt.Unix() != 1800003600 {
		t.Fatal("default expiry should be one hour", response.ExpiresAt)
	}
	return response.URL
}

func TestSignedURLRetrieval(t *testing.T) {
	handler, _ := signedTestHandler(t, "secret", "https://images.example.org")
	otherReplica, _ := signedTestHandler(t, "secret", "")
	for _, id := range []string{"student-A", "student B+#?"} {
		target := issueTestURL(t, handler, id)
		if !strings.HasPrefix(target, "https://images.example.org/") {
			t.Fatal("public base URL missing", target)
		}
		for _, replica := range []http.Handler{handler, otherReplica} {
			w := signedRequest(replica, http.MethodGet, target, "")
			if w.Code != 200 || w.Body.String() != "photo bytes" || w.Header().Get("Cache-Control") != "private, max-age=3600" {
				t.Fatal(w.Code, w.Body.String(), w.Header())
			}
			w = signedRequest(replica, http.MethodHead, target, "")
			if w.Code != 200 || w.Body.Len() != 0 {
				t.Fatal(w.Code, w.Body.String())
			}
		}
	}
	target := issueTestURL(t, otherReplica, "student-A")
	if !strings.HasPrefix(target, "/student-A?") {
		t.Fatal("unset base URL should return a relative URL", target)
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Range", "bytes=0-4")
	w := httptest.NewRecorder()
	otherReplica.ServeHTTP(w, req)
	if w.Code != 206 || w.Body.String() != "photo" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestSignedURLCannotBeAlteredOrExpired(t *testing.T) {
	handler, signer := signedTestHandler(t, "secret", "")
	target := issueTestURL(t, handler, "student-A")
	u, err := url.Parse(target)
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]string{
		"another ID":          strings.Replace(target, "/student-A?", "/student-B?", 1),
		"metrics":             strings.Replace(target, "/student-A?", "/metrics?", 1),
		"later expiry":        strings.Replace(target, "1800003600", "1800003601", 1),
		"missing signature":   "/student-A?expires=1800003600",
		"duplicate expiry":    target + "&expires=1800003600",
		"duplicate signature": target + "&signature=" + u.Query().Get("signature"),
		"malformed signature": "/student-A?expires=1800003600&signature=bad",
	}
	for name, target := range tests {
		t.Run(name, func(t *testing.T) {
			w := signedRequest(handler, http.MethodGet, target, "")
			if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal(w.Code, w.Header())
			}
		})
	}
	if w := signedRequest(handler, http.MethodPost, target, ""); w.Code != 401 {
		t.Fatal("signature permitted POST", w.Code)
	}
	rotated, _ := signedTestHandler(t, "different-secret", "")
	if w := signedRequest(rotated, http.MethodGet, target, ""); w.Code != 401 {
		t.Fatal("URL remained valid after key rotation", w.Code)
	}
	signer.now = func() time.Time { return time.Unix(1800003590, 0) }
	if w := signedRequest(handler, http.MethodGet, target, ""); w.Code != 200 || w.Header().Get("Cache-Control") != "private, max-age=10" {
		t.Fatal("cache lifetime exceeded expiry", w.Code, w.Header())
	}
	signer.now = func() time.Time { return time.Unix(1800003600, 0) }
	if w := signedRequest(handler, http.MethodGet, target, ""); w.Code != 401 {
		t.Fatal("expired URL still worked", w.Code)
	}
}

func TestPresignRequiresAPIKeyAndValidID(t *testing.T) {
	handler, _ := signedTestHandler(t, "secret", "")
	tests := []struct {
		method, target, key string
		status              int
	}{
		{"POST", "/presign?id=student-A", "", 401},
		{"POST", "/presign?id=student-A", "wrong", 401},
		{"GET", "/presign?id=student-A", "secret", 405},
		{"POST", "/presign?id=missing", "secret", 404},
		{"POST", "/presign", "secret", 400},
		{"POST", "/presign?id=../student-A", "secret", 400},
		{"POST", "/presign?id=student-A&id=student-B", "secret", 400},
		{"POST", "/presign?id=student-A&expires_in=0", "secret", 400},
		{"POST", "/presign?id=student-A&expires_in=3601", "secret", 400},
		{"POST", "/presign?id=student-A&expires_in=", "secret", 400},
		{"POST", "/presign?id=student-A&expires_in=abc", "secret", 400},
		{"POST", "/presign?id=student-A&expires_in=60&expires_in=60", "secret", 400},
		{"POST", "/presign?id=student-A&expires_in=60", "secret", 200},
	}
	for _, tt := range tests {
		w := signedRequest(handler, tt.method, tt.target, tt.key)
		if w.Code != tt.status {
			t.Errorf("%s: got %d want %d", tt.target, w.Code, tt.status)
		}
	}
}
