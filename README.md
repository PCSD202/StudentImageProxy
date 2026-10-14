# StudentImageProxy

A small HTTP server that serves JPEGs from a mounted image directory after checking an `X-API-Key` header. It defaults to `/Images` and port `80` to match the previous container.

For a request such as `/1114892`, the server opens `/Images/1114892.JPG`, then `/Images/1114892.jpg`. Requests with `.JPG` or `.jpg` also work; when both files exist, an explicit extension takes priority. IDs can contain letters and other filename characters but cannot contain path separators. Missing IDs return `404`. `/health` returns `200` without an API key.

## Run

Mount the SMB share on the Docker host, then mount the image directory into the container:

```sh
docker run --rm -p 8080:80 \
  -e API_KEY='your-secret-key' \
  -v /path/to/mounted/share:/Images:ro \
  ghcr.io/pcsd202/studentimageproxy:latest
```

```sh
curl -H 'X-API-Key: your-secret-key' http://localhost:8080/1114892
```

`API_KEY` is required. `IMAGE_DIR` and `PORT` are optional for running outside Docker. The image mount must be readable by the container. The server supports `GET`, `HEAD`, byte ranges, and conditional requests. Successful responses have `Content-Type: image/jpeg` and a private one-month cache lifetime when authenticated with an API key.

## Signed image URLs

Your backend can create a URL for one student image, then pass it to a browser for use in an `<img src="...">`. The browser does not need the API key. Set `PUBLIC_BASE_URL` to the image service's public origin, such as `https://images.example.org`. When unset, the response contains a relative URL for your backend to combine with that origin. The app uses this configured address rather than inferring it from ingress headers.

```sh
curl -X POST -H 'X-API-Key: your-secret-key' \
  'https://images.example.org/presign?id=1114892'
```

```json
{
  "url": "https://images.example.org/1114892?expires=1800003600&signature=...",
  "expires_at": "2027-01-15T09:00:00Z"
}
```

URLs default to one hour. Supply `expires_in` in seconds for a shorter lifetime, from 1 to 3600. URL-encode the `id` query parameter; bare IDs and IDs ending in `.jpg` or `.JPG` work. An ID absent from the current filename index returns `404`. The signing endpoint requires the API key and returns `Cache-Control: no-store`.

The signature authenticates the requested image path and expiration using HMAC-SHA256 with a signing key derived from `API_KEY`. It permits `GET` and `HEAD`, including ranges, and does not grant access to `/metrics` or `/presign`. Every Kubernetes replica must use the same API key; rotating it invalidates existing URLs. Anyone holding a URL can retrieve that image until it expires, so your backend should issue it only after checking the client's access to that student. Expiration prevents future retrievals; it cannot revoke bytes already downloaded.

Signed image responses use a private browser cache lifetime capped at the URL's remaining validity. Cached image bytes remain shared with API-key requests inside the server; authentication is still checked for every request. Use HTTPS for the public image service. Browsers can load these URLs directly in image tags without CORS configuration; JavaScript access to response bytes across origins requires CORS configuration at your ingress.

## CI and publishing

GitHub Actions runs Go tests and builds the container for pull requests. Pushes to `main` publish `ghcr.io/pcsd202/studentimageproxy:latest` and a commit tag. Tags matching `v*` publish a version tag. The workflow uses `GITHUB_TOKEN` with `packages: write`; the PCSD202 organization must allow this repository to publish its GHCR package. No separate registry secret is needed.

## Performance and Kubernetes

Each pod indexes JPEG filenames on startup in a Go map. The index resolves extension case and returns missing IDs without SMB file operations. It refreshes every five minutes; a failed scan retains the last successful index. The first request for a photo reads the share, and subsequent requests serve cached bytes using Go's `http.ServeContent`. Concurrent requests for the same uncached image share one read. Cached photos expire after 24 hours; their expiry is not extended by requests.

The byte budget includes image contents, keys, and an allowance for cache metadata. Least recently used entries are evicted as the budget fills. It is a cache budget, not a limit on total process memory: the filename index, active responses, Go runtime, and temporary reads also consume memory. At most four images load into memory concurrently. Files larger than the smaller of 8 MiB or one eighth of the cache budget stream from the share instead.

| Setting | Default | Purpose |
| --- | --- | --- |
| `MEMORY_LIMIT_BYTES` | Unset | Pod memory limit in bytes; derives the cache budget and Go soft memory limit |
| `CACHE_MAX_BYTES` | 64 MiB, or half `MEMORY_LIMIT_BYTES` | Cache byte budget; `0` disables image caching |
| `CACHE_TTL` | `24h` | How long cached image bytes remain valid |
| `INDEX_REFRESH_INTERVAL` | `5m` | How often new and removed filenames become visible |
| `GOMEMLIMIT` | 80% of `MEMORY_LIMIT_BYTES`, when supplied | Go's soft memory limit; an explicit value takes priority |

[deploy/kubernetes.yaml](deploy/kubernetes.yaml) provides an example using a `student-images` PVC and an API key from the `student-image-proxy` Secret's `api-key` entry. Its Downward API passes `limits.memory` into `MEMORY_LIMIT_BYTES`: a 256 MiB pod automatically gets a 128 MiB cache and an approximately 205 MiB Go soft limit at startup. Kubernetes enforces the total container memory limit; Go's GC cannot evict live cache entries itself. Environment values are read at startup, so restart pods after changing resource limits. Each replica has an independent cache. A restart clears cached photos immediately; browser cache lifetimes are independent of the server cache.

`GET /metrics` uses the same `X-API-Key` authentication and exposes Prometheus cache budget, cost, hits, misses, evictions, and indexed filename count. `/health` indicates that the HTTP server started after its initial successful index scan; it does not check current SMB availability on every probe.

Run `go test -run '^$' -bench BenchmarkImageHTTP -benchmem` to measure a complete 64 KiB response over local HTTP. This measures application and local HTTP overhead; production latency also includes ingress, client networking, and SMB reads on cache misses.
