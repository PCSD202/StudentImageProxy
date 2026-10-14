package main

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// BenchmarkImageHTTP includes a complete 64 KiB response over local HTTP.
func BenchmarkImageHTTP(b *testing.B) {
	for _, name := range []string{"photo.JPG", "photo.jpg"} {
		b.Run(name, func(b *testing.B) {
			dir := b.TempDir()
			if err := os.WriteFile(filepath.Join(dir, name), make([]byte, 64<<10), 0600); err != nil {
				b.Fatal(err)
			}
			images, err := newImageStore(dir, config{cacheBytes: 1 << 20, cacheTTL: time.Hour})
			if err != nil {
				b.Fatal(err)
			}
			server := httptest.NewServer(newHandler(images, "secret"))
			defer server.Close()
			req, err := http.NewRequest(http.MethodGet, server.URL+"/photo", nil)
			if err != nil {
				b.Fatal(err)
			}
			req.Header.Set("X-API-Key", "secret")
			b.ReportAllocs()
			b.SetBytes(64 << 10)
			b.ResetTimer()
			for b.Loop() {
				resp, err := server.Client().Do(req)
				if err != nil {
					b.Fatal(err)
				}
				_, err = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if err != nil || resp.StatusCode != http.StatusOK {
					b.Fatalf("status=%d, read error=%v", resp.StatusCode, err)
				}
			}
		})
	}
}

// BenchmarkImageHandler excludes client networking and consumes the response
// without buffering its bytes, to isolate the cache and HTTP handler overhead.
func BenchmarkImageHandler(b *testing.B) {
	dir := b.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), make([]byte, 64<<10), 0600); err != nil {
		b.Fatal(err)
	}
	images, err := newImageStore(dir, config{cacheBytes: 1 << 20, cacheTTL: time.Hour})
	if err != nil {
		b.Fatal(err)
	}
	handler := newHandler(images, "secret")
	for _, path := range []string{"/photo", "/missing"} {
		b.Run(path, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-API-Key", "secret")
			w := &discardResponse{header: make(http.Header)}
			handler.ServeHTTP(w, req)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				clear(w.header)
				handler.ServeHTTP(w, req)
			}
		})
	}
}

type discardResponse struct{ header http.Header }

func (w *discardResponse) Header() http.Header            { return w.header }
func (w *discardResponse) WriteHeader(int)                {}
func (w *discardResponse) Write(data []byte) (int, error) { return len(data), nil }

func BenchmarkSignedAuthorization(b *testing.B) {
	signer := newSigner("secret", "")
	expires := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	query := url.Values{"expires": {expires}, "signature": {base64.RawURLEncoding.EncodeToString(signer.signature("/student-A", expires))}}
	req := httptest.NewRequest(http.MethodGet, "/student-A?"+query.Encode(), nil)
	w := &discardResponse{header: make(http.Header)}
	b.ReportAllocs()
	for b.Loop() {
		if !signer.authorize(w, req) {
			b.Fatal("signature rejected")
		}
	}
}
