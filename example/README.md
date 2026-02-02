# Example Setup

This folder contains a Docker Compose setup that showcases the Traefik plugin and the
Rust sidecar dashboard.

## Run

From `example/`:

```bash
docker-compose up --build
```

## Access

- Traefik dashboard: http://localhost:8080
- Test app: http://localhost
- Stats dashboard: http://localhost/stats
- Sidecar dashboard: http://localhost:7070/stats

## Notes

- The sidecar uses a Docker volume for DuckDB storage.
- The plugin is mounted from `../traefik-stats`.
