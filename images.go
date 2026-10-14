package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jellydator/ttlcache/v3"
	"golang.org/x/sync/singleflight"
)

type imageData struct {
	data     []byte
	modified time.Time
}

type imageIndex struct {
	names map[string]struct{}
}

type imageStore struct {
	dir       string
	index     atomic.Pointer[imageIndex]
	cache     *ttlcache.Cache[string, imageData]
	budget    uint64
	maxFile   int64
	loads     singleflight.Group
	loadSlots chan struct{}
}

func newImageStore(dir string, c config) (*imageStore, error) {
	s := &imageStore{dir: dir, budget: c.cacheBytes, maxFile: min(8<<20, int64(c.cacheBytes/8)), loadSlots: make(chan struct{}, 4)}
	s.cache = ttlcache.New[string, imageData](
		ttlcache.WithTTL[string, imageData](c.cacheTTL),
		ttlcache.WithDisableTouchOnHit[string, imageData](),
		ttlcache.WithMaxCost[string, imageData](max(1, c.cacheBytes), func(item ttlcache.CostItem[string, imageData]) uint64 {
			return uint64(len(item.Value.data) + len(item.Key) + 256)
		}),
	)
	return s, s.refresh()
}

// Scan filenames, not file contents or per-file metadata on the SMB share.
// Publish a complete replacement so requests never wait for a scan.
func (s *imageStore) refresh() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	index := &imageIndex{names: make(map[string]struct{}, len(entries))}
	for _, entry := range entries {
		name := entry.Name()
		if entry.Type().IsRegular() && (strings.HasSuffix(name, ".JPG") || strings.HasSuffix(name, ".jpg")) {
			index.names[name] = struct{}{}
		}
	}
	s.index.Store(index)
	return nil
}

func (s *imageStore) refreshEvery(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if err := s.refresh(); err != nil {
			log.Printf("refresh image index: %v", err)
		}
		s.cache.DeleteExpired()
	}
}

func (s *imageStore) serve(w http.ResponseWriter, r *http.Request) error {
	index := s.index.Load()
	for _, name := range imageCandidates(r.URL.Path) {
		if _, found := index.names[name]; !found {
			continue
		}
		if item := s.cache.Get(name); item != nil {
			return serveImage(w, r, name, item.Value())
		}
		value, err, _ := s.loads.Do(name, func() (any, error) { return s.load(name) })
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if image, ok := value.(imageData); ok {
			return serveImage(w, r, name, image)
		}
		return s.stream(w, r, name)
	}
	return os.ErrNotExist
}

// Large files stream directly rather than consume the cache budget.
func (s *imageStore) stream(w http.ResponseWriter, r *http.Request, name string) error {
	file, err := os.Open(filepath.Join(s.dir, name))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return os.ErrNotExist
	}
	imageHeaders(w)
	http.ServeContent(w, r, name, info.ModTime(), file)
	return nil
}

func (s *imageStore) load(name string) (any, error) {
	// Limit temporary allocations during simultaneous misses for different IDs.
	s.loadSlots <- struct{}{}
	defer func() { <-s.loadSlots }()
	if item := s.cache.Get(name); item != nil {
		return item.Value(), nil
	}
	file, err := os.Open(filepath.Join(s.dir, name))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, os.ErrNotExist
	}
	if s.budget == 0 || info.Size() > s.maxFile {
		return nil, nil
	}
	// Allocate once, and fall back to streaming if the file changed size.
	data := make([]byte, info.Size())
	if _, err := io.ReadFull(file, data); err != nil {
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, nil
		}
		return nil, err
	}
	var extra [1]byte
	if _, err := file.Read(extra[:]); err != io.EOF {
		return nil, err
	}
	image := imageData{data: data, modified: info.ModTime()}
	s.cache.Set(name, image, ttlcache.DefaultTTL)
	return image, nil
}

func imageHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "image/jpeg")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "private, max-age=2592000")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func serveImage(w http.ResponseWriter, r *http.Request, name string, image imageData) error {
	imageHeaders(w)
	http.ServeContent(w, r, name, image.modified, bytes.NewReader(image.data))
	return nil
}

func (s *imageStore) metrics(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	m := s.cache.Metrics()
	var cost uint64
	for _, item := range s.cache.Items() {
		cost += item.Cost()
	}
	fmt.Fprintf(w, "image_cache_budget_bytes %d\nimage_cache_cost_bytes %d\nimage_cache_items %d\nimage_cache_hits_total %d\nimage_cache_misses_total %d\nimage_cache_evictions_total %d\nimage_index_files %d\n", s.budget, cost, s.cache.Len(), m.Hits, m.Misses, m.Evictions, len(s.index.Load().names))
}
