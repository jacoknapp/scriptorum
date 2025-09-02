# Scriptorum (Overseerr-style for Books)

Royal-purple web UI to request eBooks/audiobooks, bridging:
- **Amazon (public)**: auto-detect ASINs & scrape detail pages; keyword search via public search results
- **Open Library**: keyword search metadata
- **Readarr v1**: separate instances for eBooks & audiobooks, identifier-first match (ISBN-13 → ISBN-10 → ASIN)
- **Audiobookshelf**: presence badge on search results

## Run (Docker)
```bash
docker compose up -d --build
open http://localhost:8080
```

## First-run
- Setup wizard collects Admin, OAuth (optional), Scriptorum, and Readarr endpoints.
- SQLite DB is initialized at `/data/scriptorum.db` and a default config is generated at `/data/config.yaml`.

## CI/CD
GitHub Actions builds & tests Go, then builds & pushes multi-arch images to Docker Hub:
- Tags: `latest`, short `sha`, and release tag.
Secrets required:
- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`

## Dev
```bash
make build
make test
make docker-run
```

## Notes
- Amazon HTML can change; selectors are resilient but may need tweaks.
- Amazon is used **automatically** (no keys). Open Library results are merged with Amazon results (deduped).
