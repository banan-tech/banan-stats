# Troubleshooting Docker Compose Setup

## Common Issues and Fixes

### Plugin Not Loading

If the plugin isn't loading, check:

1. **Module Path**: The `moduleName` in `traefik-config.yml` should match the module path in `traefik-stats/go.mod`:
   ```yaml
   experimental:
     localPlugins:
       banan-stats:
         moduleName: github.com/khaled/banan-stats/traefik-stats
   ```

2. **Volume Mount**: The plugin source must be mounted at the correct path:
   ```yaml
   volumes:
     - ./traefik-stats:/plugins-local/src/github.com/khaled/banan-stats/traefik-stats
   ```

3. **Check Traefik Logs**: Look for plugin loading errors:
   ```bash
   docker-compose logs traefik | grep -i plugin
   ```

### /stats Endpoint Not Working

The `/stats` endpoint requires:

1. **Router Configuration**: A router must be defined in `traefik-dynamic.yml`:
   ```yaml
   routers:
     stats:
       rule: "Path(`/stats`) || PathPrefix(`/stats/`)"
       entryPoints:
         - web
       middlewares:
         - stats
       service: stats-service
   ```

2. **Service Configuration**: A service must be defined (even though middleware intercepts):
   ```yaml
   services:
     stats-service:
       loadBalancer:
         servers:
           - url: "http://sidecar:7070"
   ```

3. **Middleware Configuration**: The middleware must have `dashboardPath` set:
   ```yaml
   middlewares:
     stats:
       plugin:
         banan-stats:
           dashboardPath: "/stats"
           sidecarURL: "http://sidecar:7070"
   ```

### Debugging Steps

1. **Check all container logs**:
   ```bash
   docker-compose logs
   ```

2. **Check Traefik configuration**:
   ```bash
   docker-compose exec traefik cat /etc/traefik/traefik.yml
   docker-compose exec traefik cat /etc/traefik/dynamic.yml
   ```

3. **Verify plugin is loaded**:
   - Visit http://localhost:8080 (Traefik dashboard)
   - Check if the plugin appears in the plugins list

4. **Test sidecar directly**:
   ```bash
   curl http://localhost:7070/stats
   ```

5. **Test through Traefik**:
   ```bash
   curl http://localhost/stats
   ```

### Network Issues

If services can't communicate:

1. **Verify all services are on the same network**:
   ```bash
   docker network inspect banan-stats_banan-stats-network
   ```

2. **Test connectivity**:
   ```bash
   docker-compose exec traefik wget -O- http://sidecar:7070/stats
   ```
