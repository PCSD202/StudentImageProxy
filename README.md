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

`API_KEY` is required. `IMAGE_DIR` and `PORT` are optional for running outside Docker. The image mount must be readable by the container. The server supports `GET`, `HEAD`, byte ranges, and conditional requests. Successful responses have `Content-Type: image/jpeg` and a private one-month cache lifetime.

## CI and publishing

GitHub Actions runs Go tests and builds the container for pull requests. Pushes to `main` publish `ghcr.io/pcsd202/studentimageproxy:latest` and a commit tag. Tags matching `v*` publish a version tag. The workflow uses `GITHUB_TOKEN` with `packages: write`; the PCSD202 organization must allow this repository to publish its GHCR package. No separate registry secret is needed.
