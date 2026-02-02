## Usage

### Sidecar

Run locally:

```
cd banan-stats
cargo run --manifest-path ./Cargo.toml -- --db-path ./clj_simple_stats.duckdb --listen :7070
```

Run via Docker:

```
cd banan-stats
docker build -f Dockerfile -t banan-stats-sidecar .
docker run --rm -p 7070:7070 -v "$PWD:/data" banan-stats-sidecar --db-path /data/clj_simple_stats.duckdb
```

### Traefik plugin

1. Configure the plugin repository (point Traefik to `traefik-stats`).
2. Add a middleware configuration in your dynamic config:

```yaml
http:
  middlewares:
    stats:
      plugin:
        banan-stats:
          sidecarURL: "http://localhost:7070"
          dashboardPath: "/stats"
          dashboardToken: ""
          cookieName: "stats_id"
          queueSize: 1024
          flushInterval: "2s"
```

3. Attach the middleware to routers that serve HTML/RSS.

### Dashboard access

If `dashboardToken` is set, pass `Authorization: Bearer <token>` when accessing `/stats`.
