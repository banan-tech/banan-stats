# Docker Compose End-to-End Testing Setup

This docker-compose file sets up a complete testing environment for banan-stats with:
- **sidecar**: The stats sidecar service running HTTP on 7070
- **traefik**: Traefik v3 with the banan-stats plugin installed
- **test-app**: A simple nginx container for testing

## Usage

1. Start all services:
```bash
docker-compose up --build
```

2. Access the services:
   - Traefik dashboard: http://localhost:8080
   - Test application: http://localhost
   - Stats dashboard: http://localhost/stats
   - Sidecar dashboard: http://localhost:7070/stats

3. Stop all services:
```bash
docker-compose down
```

4. Clean up volumes (removes database):
```bash
docker-compose down -v
```

## Configuration

- The sidecar stores its DuckDB database in a Docker volume (`sidecar-data`)
- Traefik loads the plugin from the local `traefik-stats` directory
- The plugin configuration is in `traefik-dynamic.yml`
- The test-app uses the stats middleware automatically

## Testing

1. Visit http://localhost to generate some traffic
2. Check the stats dashboard at http://localhost/stats
3. Monitor Traefik logs: `docker-compose logs -f traefik`
4. Monitor sidecar logs: `docker-compose logs -f sidecar`
