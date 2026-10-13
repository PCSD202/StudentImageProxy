package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	key := os.Getenv("API_KEY")
	if key == "" {
		log.Fatal("API_KEY must be set")
	}

	imageDir := os.Getenv("IMAGE_DIR")
	if imageDir == "" {
		imageDir = "/Images"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           newHandler(imageDir, key),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("serving images from %s on port %s", imageDir, port)
	log.Fatal(server.ListenAndServe())
}

func newHandler(imageDir, key string) http.Handler {
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

		candidates := imageCandidates(r.URL.Path)
		if len(candidates) == 0 {
			http.NotFound(w, r)
			return
		}
		for _, name := range candidates {
			file, err := os.Open(filepath.Join(imageDir, name))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				log.Printf("open image %q: %v", name, err)
				http.Error(w, "image unavailable", http.StatusInternalServerError)
				return
			}

			info, err := file.Stat()
			if err != nil {
				file.Close()
				log.Printf("stat image %q: %v", name, err)
				http.Error(w, "image unavailable", http.StatusInternalServerError)
				return
			}
			if !info.Mode().IsRegular() {
				file.Close()
				continue
			}

			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Cache-Control", "private, max-age=2592000")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			http.ServeContent(w, r, name, info.ModTime(), file)
			file.Close()
			return
		}
		http.NotFound(w, r)
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
