# Logging Configuration

Bosun uses structured logging throughout the codebase, providing consistent, queryable logs for debugging and monitoring.

## Output Modes

### Console Mode (Default for CLI)

Human-readable colored output suitable for interactive use:

```
2024-01-15T10:30:00Z INF Starting reconciliation source=webhook reconcile_id=abc123
2024-01-15T10:30:05Z INF Reconciliation completed duration_ms=5000 success=true
```

### JSON Mode (Daemon)

Structured JSON output for log aggregation systems:

```json
{"level":"info","time":"2024-01-15T10:30:00Z","component":"reconcile","reconcile_id":"abc123","source":"webhook","msg":"Starting reconciliation"}
{"level":"info","time":"2024-01-15T10:30:05Z","component":"reconcile","reconcile_id":"abc123","duration_ms":5000,"success":true,"msg":"Reconciliation completed"}
```

The daemon automatically uses JSON mode. Set `BOSUN_DAEMON_MODE=true` to force JSON output.

## Log Levels

| Level | Usage |
|-------|-------|
| `debug` | Detailed debugging information |
| `info` | Normal operational messages |
| `warn` | Warning conditions, potential issues |
| `error` | Error conditions that don't halt operation |
| `fatal` | Critical errors that cause shutdown |

Set log level with `BOSUN_LOG_LEVEL`:

```bash
BOSUN_LOG_LEVEL=debug bosun daemon
```

## Standard Fields

All log entries include consistent field names for easy querying:

| Field | Description | Example |
|-------|-------------|---------|
| `component` | Source component | `daemon`, `reconcile`, `docker` |
| `reconcile_id` | Correlation ID for reconcile runs | `abc123def` |
| `request_id` | HTTP request correlation ID | `req-456` |
| `duration_ms` | Operation duration in milliseconds | `1234` |
| `path` | File or socket path | `/var/run/bosun.sock` |
| `container` | Container name | `traefik` |
| `operation` | Operation being performed | `restart`, `deploy` |

## Component Logging

### Daemon

```json
{"level":"info","component":"daemon","msg":"Daemon starting","version":"0.2.10","socket":"/var/run/bosun.sock"}
{"level":"info","component":"daemon","msg":"Received shutdown signal","signal":"SIGTERM"}
```

### Reconcile

```json
{"level":"info","component":"reconcile","reconcile_id":"abc123","source":"webhook","msg":"Starting reconciliation"}
{"level":"info","component":"deploy","reconcile_id":"abc123","duration_ms":1500,"msg":"Deployment completed"}
```

### Docker

```json
{"level":"info","component":"docker","container":"traefik","operation":"restart","msg":"Restarting container"}
{"level":"info","component":"docker","container":"traefik","msg":"Container restarted successfully"}
```

### HTTP/Webhooks

```json
{"level":"info","component":"http","request_id":"req-123","method":"POST","url":"/webhook","status":202,"duration_ms":5}
{"level":"warn","component":"http","request_id":"req-124","remote_addr":"1.2.3.4","msg":"Webhook signature validation failed"}
```

## Security Event Logging

Security-sensitive operations are logged at `warn` level for audit purposes:

```json
{"level":"warn","component":"http","remote_addr":"1.2.3.4","endpoint":"/webhook","msg":"Webhook signature validation failed"}
{"level":"warn","operation":"overboard","container":"malicious-container","msg":"Emergency container removal initiated"}
{"level":"warn","operation":"rollback","snapshot":"snapshot-20240115","msg":"Rollback initiated"}
```

## Log Aggregation

Bosun's JSON logs are compatible with common log aggregation systems:

### Loki/Grafana

```yaml
# promtail config
scrape_configs:
  - job_name: bosun
    static_configs:
      - targets: [localhost]
        labels:
          job: bosun
          __path__: /var/log/bosun/*.log
```

### Elasticsearch/Kibana

```json
{
  "type": "log",
  "enabled": true,
  "paths": ["/var/log/bosun/*.log"],
  "json.keys_under_root": true,
  "json.add_error_key": true
}
```

### CloudWatch

Use the awslogs driver with JSON parsing enabled.

## Troubleshooting

### Enable debug logging

```bash
BOSUN_LOG_LEVEL=debug bosun daemon
```

### Follow daemon logs

```bash
journalctl -u bosun -f
# or
tail -f /var/log/bosun/daemon.log
```

### Correlate requests

Use `reconcile_id` or `request_id` fields to trace operations:

```bash
jq 'select(.reconcile_id == "abc123")' /var/log/bosun/daemon.log
```

### Filter by component

```bash
jq 'select(.component == "docker")' /var/log/bosun/daemon.log
```
