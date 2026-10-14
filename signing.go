package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const signedURLLifetime = time.Hour

type urlSigner struct {
	apiKeyHash [32]byte
	signingKey []byte
	baseURL    string
	now        func() time.Time
}

func newSigner(key, baseURL string) *urlSigner {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("StudentImageProxy signed URLs v1"))
	return &urlSigner{apiKeyHash: sha256.Sum256([]byte(key)), signingKey: mac.Sum(nil), baseURL: baseURL, now: time.Now}
}

func (s *urlSigner) hasAPIKey(r *http.Request) bool {
	provided := sha256.Sum256([]byte(r.Header.Get("X-API-Key")))
	return subtle.ConstantTimeCompare(s.apiKeyHash[:], provided[:]) == 1
}

// Both GET and HEAD authorize retrieval of the same image. Expiry and path
// are authenticated; changing either invalidates the signature.
func (s *urlSigner) signature(path, expires string) []byte {
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte("GET\n" + expires + "\n" + path))
	return mac.Sum(nil)
}

func (s *urlSigner) authorize(w http.ResponseWriter, r *http.Request) bool {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || len(imageCandidates(r.URL.Path)) == 0 {
		return false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["expires"]) != 1 || len(query["signature"]) != 1 {
		return false
	}
	expires := query.Get("expires")
	expiry, err := strconv.ParseInt(expires, 10, 64)
	now := s.now().Unix()
	if err != nil || expiry <= now {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(query.Get("signature"))
	if err != nil || !hmac.Equal(signature, s.signature(r.URL.Path, expires)) {
		return false
	}
	w.Header().Set("Cache-Control", "private, max-age="+strconv.FormatInt(expiry-now, 10))
	return true
}

// POST /presign?id=student-ID&expires_in=3600 requires the backend API key.
func (s *urlSigner) presign(w http.ResponseWriter, r *http.Request, images *imageStore) {
	w.Header().Set("Cache-Control", "no-store")
	if !s.hasAPIKey(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(query["id"]) != 1 || len(query["expires_in"]) > 1 {
		http.Error(w, "provide one image id and an optional expires_in", http.StatusBadRequest)
		return
	}
	path := "/" + query.Get("id")
	candidates := imageCandidates(path)
	if len(candidates) == 0 || path == "/health" || path == "/metrics" || path == "/presign" {
		http.Error(w, "invalid image id", http.StatusBadRequest)
		return
	}
	seconds := int64(signedURLLifetime / time.Second)
	if values, supplied := query["expires_in"]; supplied {
		seconds, err = strconv.ParseInt(values[0], 10, 64)
		if err != nil || seconds <= 0 || seconds > int64(signedURLLifetime/time.Second) {
			http.Error(w, "expires_in must be between 1 and 3600 seconds", http.StatusBadRequest)
			return
		}
	}
	index := images.index.Load()
	if _, upper := index.names[candidates[0]]; !upper {
		if _, lower := index.names[candidates[1]]; !lower {
			http.NotFound(w, r)
			return
		}
	}
	expiresAt := s.now().Add(time.Duration(seconds) * time.Second).Truncate(time.Second)
	expires := strconv.FormatInt(expiresAt.Unix(), 10)
	imageURL := url.URL{Path: path, RawQuery: url.Values{
		"expires": {expires}, "signature": {base64.RawURLEncoding.EncodeToString(s.signature(path, expires))},
	}.Encode()}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		URL       string    `json:"url"`
		ExpiresAt time.Time `json:"expires_at"`
	}{URL: s.baseURL + imageURL.String(), ExpiresAt: expiresAt})
}
