## Implementation details

### Overview

The Traefik plugin collects request/response metadata and asynchronously batches events
to a sidecar. The sidecar owns DuckDB, enriches events via the analyzer, and renders the
server-side dashboard.

### Data flow

1. Request passes through the middleware.
2. If the response is loggable (200 + HTML/RSS/Atom), an event is enqueued.
3. A background worker batches events and streams them to the sidecar over HTTP.
4. The sidecar enriches each event (agent/type/os/mult/uniq/ref_domain) and inserts into DuckDB.
5. `GET /stats` renders the dashboard using DuckDB queries.

### Multi-domain support

Each event includes `host` (without port). The dashboard includes host filters, and
all queries accept host filters via query parameters.

### Schema

```sql
CREATE TABLE stats (
  date       DATE,
  time       TIME,
  host       VARCHAR,
  path       VARCHAR,
  query      VARCHAR,
  ip         VARCHAR,
  user_agent VARCHAR,
  referrer   VARCHAR,
  type       agent_type_t,
  agent      VARCHAR,
  os         agent_os_t,
  ref_domain VARCHAR,
  mult       INTEGER,
  set_cookie UUID,
  uniq       UUID
);
```

### Sidecar internals

- DuckDB connection pooling uses a single connection for consistency.
- Inserts are transactional and update `uniq` for second visits.
- Dashboard queries mirror the original Clojure implementation, including `MAX(mult)` for RSS.

### Plugin internals

- Uses a bounded channel for async batching.
- Sets the tracking cookie before the upstream handler runs to avoid buffering responses.
- Protects the dashboard with an optional bearer token.
