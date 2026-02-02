# Banan Stats

This workspace contains the Rust sidecar and the Go Traefik plugin:

- `banan-stats/` — Rust sidecar service that stores and renders stats
- `traefik-stats/` — Traefik v3 middleware plugin that ships events to the sidecar
- `example/` — Docker Compose setup that showcases the plugin and sidecar

## Quick start

1. Run the sidecar:

```
cd banan-stats
cargo run --manifest-path ./Cargo.toml -- --db-path ./clj_simple_stats.duckdb --listen :7070
```

2. Configure Traefik to use the plugin from `traefik-stats` and point it to the sidecar.

See `docs/usage.md` for full examples.
