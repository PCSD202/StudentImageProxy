package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	key := os.Getenv("API_KEY")
	if key == "" {
		log.Fatal("API_KEY must be set")
	}

	imageDir := os.Getenv("IMAGE_DIR")
	if imageDir == "" {
		imageDir = "/Images"
	}
	images, err := newImageStore(imageDir, config)
	if err != nil {
		log.Fatal(err)
	}
	go images.refreshEvery(config.indexRefresh)
	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           newHandler(images, key),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("serving %d indexed images from %s on port %s; cache budget %d bytes", len(images.index.Load().names), imageDir, port, config.cacheBytes)
	log.Fatal(server.ListenAndServe())
}

func newHandler(images *imageStore, key string) http.Handler {
	expectedKey := sha256.Sum256([]byte(key))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("ok\n"))
			return
		}

		providedKey := sha256.Sum256([]byte(r.Header.Get("X-API-Key")))
		if subtle.ConstantTimeCompare(expectedKey[:], providedKey[:]) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/metrics" {
			images.metrics(w)
			return
		}
		if err := images.serve(w, r); errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
		} else if err != nil {
			log.Printf("serve image: %v", err)
			http.Error(w, "image unavailable", http.StatusInternalServerError)
		}
	})
}

// imageCandidates accepts one filename segment, optionally followed by .JPG or .jpg.
func imageCandidates(path string) []string {
	if !strings.HasPrefix(path, "/") {
		return nil
	}
	id := strings.TrimPrefix(path, "/")
	preferLowercase := strings.HasSuffix(id, ".jpg")
	if strings.HasSuffix(id, ".JPG") || preferLowercase {
		id = id[:len(id)-4]
	}
	if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\") || strings.ContainsRune(id, 0) {
		return nil
	}
	if preferLowercase {
		return []string{id + ".jpg", id + ".JPG"}
	}
	return []string{id + ".JPG", id + ".jpg"}
}
