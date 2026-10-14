package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func requestImage(t *testing.T, s *imageStore, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("X-API-Key", "secret")
	newHandler(s, "secret").ServeHTTP(w, r)
	return w
}

func TestCachedHitsAndIndexedMissesAvoidShare(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.jpg"), []byte("photo bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := newImageStore(dir, config{cacheBytes: 1 << 20, cacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if w := requestImage(t, s, "/photo"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Simulate an unavailable share after loading the image.
	s.dir = filepath.Join(dir, "unavailable")
	if w := requestImage(t, s, "/photo.JPG"); w.Code != 200 || w.Body.String() != "photo bytes" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := requestImage(t, s, "/missing"); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := s.refresh(); err == nil {
		t.Fatal("refresh should fail while share is unavailable")
	}
	if w := requestImage(t, s, "/photo"); w.Code != 200 {
		t.Fatal("failed refresh discarded index", w.Code)
	}
}

func TestIndexRefresh(t *testing.T) {
	dir := t.TempDir()
	s, err := newImageStore(dir, config{cacheBytes: 1 << 20, cacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "new.JPG")
	if err := os.WriteFile(path, []byte("new photo"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := requestImage(t, s, "/new"); w.Code != 404 {
		t.Fatal("new filename should wait for index refresh")
	}
	if err := s.refresh(); err != nil {
		t.Fatal(err)
	}
	if w := requestImage(t, s, "/new"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := s.refresh(); err != nil {
		t.Fatal(err)
	}
	if w := requestImage(t, s, "/new"); w.Code != 404 {
		t.Fatal("removed filename remained visible", w.Code)
	}
}

func TestCacheBudgetAndConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	for i := range 24 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d.JPG", i)), []byte(strings.Repeat("x", 1024)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	s, err := newImageStore(dir, config{cacheBytes: 12 << 10, cacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := range 48 {
		wg.Go(func() {
			if w := requestImage(t, s, fmt.Sprintf("/%d", i%24)); w.Code != 200 || w.Body.Len() != 1024 {
				t.Error(w.Code, w.Body.Len())
			}
		})
	}
	wg.Wait()
	var cost uint64
	for _, item := range s.cache.Items() {
		cost += item.Cost()
	}
	if cost > s.budget || s.cache.Metrics().Evictions == 0 {
		t.Fatalf("cost=%d, budget=%d, evictions=%d", cost, s.budget, s.cache.Metrics().Evictions)
	}
}

func TestCacheDisabledOrFileTooLarge(t *testing.T) {
	for _, budget := range []uint64{0, 1024} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "photo.JPG"), []byte(strings.Repeat("x", 1024)), 0600); err != nil {
				t.Fatal(err)
			}
			s, err := newImageStore(dir, config{cacheBytes: budget, cacheTTL: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			if w := requestImage(t, s, "/photo"); w.Code != 200 || w.Body.Len() != 1024 {
				t.Fatal(w.Code, w.Body.Len())
			}
			if s.cache.Len() != 0 {
				t.Fatal("uncacheable image entered the cache")
			}
		})
	}
}

func TestCachedConditionalRequest(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "photo.JPG"), []byte("photo"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := newImageStore(dir, config{cacheBytes: 1 << 20, cacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	first := requestImage(t, s, "/photo")
	request := httptest.NewRequest(http.MethodGet, "/photo", nil)
	request.Header.Set("X-API-Key", "secret")
	request.Header.Set("If-Modified-Since", first.Header().Get("Last-Modified"))
	w := httptest.NewRecorder()
	newHandler(s, "secret").ServeHTTP(w, request)
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Body.String())
	}
}
