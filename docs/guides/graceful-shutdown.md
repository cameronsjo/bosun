# Graceful Shutdown Patterns

How to stop containers without dropping requests. Organized by layer, from quick wins to infrastructure-level solutions.

## The Problem

Docker's default shutdown sequence:

```
docker stop → SIGTERM → (10s grace) → SIGKILL
```

This works fine when your requests complete in under 10 seconds. It falls apart when:

- LLM inference calls take 15-30s (or longer)
- SSE streams are open indefinitely
- Clients have no idea the server is going away
- Services depend on each other and shut down in the wrong order

The result: SIGKILL mid-stream, broken connections, unhappy users.

## Signal Flow

Understanding who receives what, and when:

```
Docker sends SIGTERM
    → PID 1 in container (usually uvicorn/gunicorn/node)
        → Application signal handler (if registered)
            → Your cleanup code runs
                → (grace period expires)
Docker sends SIGKILL
    → Process dies immediately, no cleanup
```

Key detail: SIGKILL cannot be caught, blocked, or ignored. Once Docker sends it, the process is gone. Everything you do in this guide is about making the window between SIGTERM and SIGKILL large enough and useful enough that SIGKILL never fires during real work.

## Layer 1: Buy Time

**Effort**: Trivial (config changes only)
**Effect**: Container survives long-running requests instead of getting killed mid-stream

### Docker: Increase Grace Period

In `docker-compose.yml`:

```yaml
services:
  my-service:
    stop_grace_period: 120s  # Default is 10s
```

Choose a value that exceeds your longest expected request. If your chairman model takes 30s, set this to at least 60s. If you have batch operations that take minutes, size accordingly.

> **Rule of thumb**: `stop_grace_period` SHOULD be at least 2x your longest expected request duration.

### Uvicorn: Drain In-Flight Requests

Without `--timeout-graceful-shutdown`, uvicorn receives SIGTERM and exits immediately — it does not wait for in-flight requests to complete.

```dockerfile
CMD ["uvicorn", "app:app", "--host", "0.0.0.0", "--port", "8000", \
     "--timeout-graceful-shutdown", "90"]
```

Or in a `uvicorn.config` / programmatic setup:

```python
uvicorn.run(
    "app:app",
    host="0.0.0.0",
    port=8000,
    timeout_graceful_shutdown=90,
)
```

This tells uvicorn: "On SIGTERM, stop accepting new connections but let existing requests finish, up to 90 seconds."

> **Relationship**: `timeout-graceful-shutdown` MUST be less than `stop_grace_period`. Otherwise Docker kills uvicorn before it finishes draining.

### Gunicorn: Equivalent Config

If using gunicorn with uvicorn workers:

```python
# gunicorn.conf.py
graceful_timeout = 90  # Seconds to wait for workers to finish
timeout = 120          # Max time for a single request
```

### Node.js: Equivalent Pattern

```javascript
const server = app.listen(port);

process.on('SIGTERM', () => {
  server.close(() => {
    // All connections drained
    process.exit(0);
  });
});
```

`server.close()` stops accepting new connections and waits for existing ones to finish.

### Summary: Layer 1 Config

| Setting | Where | Default | Recommended |
|---------|-------|---------|-------------|
| `stop_grace_period` | docker-compose.yml | 10s | 2x longest request |
| `--timeout-graceful-shutdown` | uvicorn CLI | None (instant exit) | grace_period - 30s |
| `graceful_timeout` | gunicorn.conf.py | 30s | grace_period - 30s |

## Layer 2: Signal Active Clients

**Effort**: Moderate (application code changes)
**Effect**: Clients know the server is restarting and can retry gracefully instead of showing raw network errors

### The Pattern

1. Track active long-lived connections (SSE streams, WebSockets)
2. Set a shutdown flag on SIGTERM
3. Active connections send a structured shutdown event
4. Clients receive the event and reconnect/retry

### Python + SSE Example

```python
import asyncio
import signal
from contextlib import asynccontextmanager

# Shutdown coordination
shutdown_event = asyncio.Event()
active_streams: set[asyncio.Task] = set()

@asynccontextmanager
async def lifespan(app):
    # Register SIGTERM handler
    loop = asyncio.get_event_loop()
    loop.add_signal_handler(signal.SIGTERM, shutdown_event.set)
    yield
    # Wait for active streams to finish (with timeout)
    if active_streams:
        await asyncio.wait(active_streams, timeout=60)

app = FastAPI(lifespan=lifespan)

async def sse_generator(request):
    task = asyncio.current_task()
    active_streams.add(task)
    try:
        async for chunk in call_llm_model(request):
            if shutdown_event.is_set():
                # Tell the client we're going away
                yield {
                    "event": "server_shutdown",
                    "data": '{"retry_after_ms": 5000}'
                }
                return
            yield {"event": "token", "data": chunk}
    finally:
        active_streams.discard(task)
```

### Frontend Handling

```typescript
const eventSource = new EventSource("/api/stream");

eventSource.addEventListener("server_shutdown", (event) => {
  const { retry_after_ms } = JSON.parse(event.data);

  // Show user-friendly message instead of error
  showNotification("Server restarting, retrying...");

  // Reconnect after delay
  setTimeout(() => reconnect(), retry_after_ms);
});
```

### WebSocket Equivalent

```python
@app.websocket("/ws")
async def websocket_endpoint(ws: WebSocket):
    await ws.accept()
    active_streams.add(asyncio.current_task())
    try:
        while not shutdown_event.is_set():
            data = await ws.receive_text()
            result = await process(data)
            await ws.send_json(result)

        # Clean close with shutdown code
        await ws.close(code=1012, reason="Server restarting")
    finally:
        active_streams.discard(asyncio.current_task())
```

WebSocket close code `1012` means "Service Restart" — clients that understand this code can auto-reconnect.

### What This Doesn't Solve

Layer 2 handles streams that are *in progress* when SIGTERM arrives. It does NOT handle:

- New requests that arrive during the drain window (Layer 1 handles this — uvicorn stops accepting)
- Requests that need to survive across restarts (Layer 4 — checkpoint/resume)
- Zero-downtime for external clients (Layer 3 — blue-green)

## Layer 3: Blue-Green Deploys

**Effort**: Infrastructure config
**Effect**: Zero downtime — old container drains while new container serves new requests

### How It Works

```
1. New container starts alongside old container
2. New container passes health check → Traefik routes new traffic to it
3. Old container receives SIGTERM
4. Old container drains existing connections (Layer 1 + 2)
5. Old container exits cleanly
```

The key insight: the old container never receives new traffic after the new one is healthy. It only needs to finish what it already started.

### Traefik + Docker Compose

```yaml
services:
  my-service:
    deploy:
      update_config:
        order: start-first       # New container starts before old one stops
        failure_action: rollback
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.my-service.rule=Host(`my-service.example.com`)"
      - "traefik.http.services.my-service.loadbalancer.healthcheck.path=/health"
      - "traefik.http.services.my-service.loadbalancer.healthcheck.interval=5s"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8000/health"]
      interval: 10s
      timeout: 5s
      retries: 3
      start_period: 30s
    stop_grace_period: 120s
```

> **Note**: `deploy.update_config.order: start-first` requires Docker Compose with `docker compose up --remove-orphans`. In Swarm mode this works natively. In standalone Compose, you may need to orchestrate the rolling update yourself or use bosun's reconcile workflow.

### Health Endpoint

The `/health` endpoint MUST distinguish between "ready to serve" and "draining":

```python
@app.get("/health")
async def health():
    if shutdown_event.is_set():
        # Tells Traefik to stop sending new traffic
        return JSONResponse(
            status_code=503,
            content={"status": "draining"}
        )
    return {"status": "healthy"}
```

This is the bridge between Layer 2 (application awareness) and Layer 3 (infrastructure routing).

## Layer 4: Preemptive Draining via Readiness

**Effort**: Low-moderate
**Effect**: Load balancer stops routing to the container *before* SIGTERM, giving it a head start on draining

### The Pattern

Separate `/health` (liveness) from `/ready` (readiness):

| Endpoint | Purpose | When it returns unhealthy |
|----------|---------|--------------------------|
| `/health` | "Is the process alive?" | Process is crashed/stuck |
| `/ready` | "Should I receive new traffic?" | Shutting down, overloaded, dependency down |

### Why This Matters

With only a health check, the sequence is:

```
SIGTERM → health returns 503 → Traefik notices (next interval) → stops routing
```

There's a gap between SIGTERM and Traefik's next health check where new requests still arrive. With a readiness probe pointing at `/ready`, you can make the container "go unready" slightly before the actual shutdown, or the load balancer can detect the draining state faster.

### Implementation

```python
is_ready = True

@app.get("/ready")
async def readiness():
    if not is_ready or shutdown_event.is_set():
        return JSONResponse(status_code=503, content={"ready": False})
    return {"ready": True}

@app.get("/health")
async def health():
    return {"status": "healthy"}  # Always healthy if process is responding
```

Configure Traefik to use `/ready` for routing decisions:

```yaml
labels:
  - "traefik.http.services.my-service.loadbalancer.healthcheck.path=/ready"
  - "traefik.http.services.my-service.loadbalancer.healthcheck.interval=2s"
```

## Layer 5: Dependency-Aware Shutdown Order

**Effort**: Moderate (compose config + optional scripting)
**Effect**: Services shut down in the right order — consumers before producers

### The Problem

If you have:

```
frontend → api → database
```

And Docker Compose stops them in arbitrary order, the API might lose its database connection mid-request. Compose `depends_on` controls *startup* order, not *shutdown* order.

### Compose Shutdown Order

Docker Compose stops services in reverse dependency order when using `depends_on`. This means if you declare:

```yaml
services:
  frontend:
    depends_on:
      - api
  api:
    depends_on:
      - database
  database:
    image: postgres
```

On `docker compose down`, the order is: frontend → api → database. This is usually correct.

### When It Breaks

`depends_on` only works within a single compose file. If services span multiple compose files or stacks, you need explicit ordering. Bosun's reconcile workflow handles this — services are deployed per-stack, and stack ordering can be controlled via the manifest system.

### Manual Ordering with Stop Scripts

For cases where compose ordering isn't enough:

```bash
#!/bin/bash
# stop-stack.sh - Graceful ordered shutdown

# 1. Stop accepting new traffic (make frontend unready)
docker exec frontend curl -X POST http://localhost:8000/admin/drain

# 2. Wait for frontend to drain
sleep 10

# 3. Stop frontend
docker compose stop frontend

# 4. Wait for API to drain (it still handles in-flight from frontend)
sleep 30

# 5. Stop everything else
docker compose down
```

## Layer 6: Checkpoint and Resume

**Effort**: High (application architecture change)
**Effect**: Long-running operations survive restarts by persisting progress

### When You Need This

When operations genuinely take longer than any reasonable grace period:

- Batch processing jobs
- Large file uploads/downloads
- Multi-step LLM pipelines (chain-of-thought across multiple models)

### The Pattern

```python
@app.post("/api/generate")
async def generate(request: GenerateRequest):
    job_id = ulid.new()

    # Persist job state
    await store.save_job(job_id, {
        "status": "in_progress",
        "request": request.dict(),
        "progress": 0,
    })

    # Run in background
    asyncio.create_task(run_generation(job_id, request))
    return {"job_id": job_id, "status_url": f"/api/jobs/{job_id}"}

async def run_generation(job_id: str, request: GenerateRequest):
    try:
        async for chunk in call_model(request):
            if shutdown_event.is_set():
                await store.update_job(job_id, {
                    "status": "interrupted",
                    "progress": current_progress,
                })
                return
            # ... yield chunk to client ...
    except Exception as e:
        await store.update_job(job_id, {"status": "failed", "error": str(e)})
```

On restart, a recovery task picks up interrupted jobs:

```python
@asynccontextmanager
async def lifespan(app):
    # Resume interrupted jobs from previous run
    interrupted = await store.get_jobs(status="interrupted")
    for job in interrupted:
        asyncio.create_task(run_generation(job.id, job.request))
    yield
```

## Choosing Your Layer

| Scenario | Minimum Layer | Recommended |
|----------|--------------|-------------|
| Standard web requests (<5s) | Layer 1 | Layer 1 |
| LLM inference (15-30s) | Layer 1 | Layer 1 + 2 |
| SSE/WebSocket streams | Layer 2 | Layer 2 + 3 |
| Production with SLA | Layer 3 | Layer 3 + 4 |
| Multi-service stack | Layer 5 | Layer 1 + 5 |
| Jobs that take minutes | Layer 6 | Layer 2 + 6 |
| "I never want users to see an error" | Layer 3 | All of them |

## Quick Reference: Config Values

```yaml
# docker-compose.yml
services:
  my-service:
    stop_grace_period: 120s
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8000/ready"]
      interval: 5s
      timeout: 3s
      retries: 3
      start_period: 30s
```

```bash
# Uvicorn
uvicorn app:app --timeout-graceful-shutdown 90

# Gunicorn
gunicorn app:app --graceful-timeout 90 --timeout 120

# Node.js: handled in code (see Layer 1)
```

## Relationship Between Timeouts

```
|-------- stop_grace_period (120s) --------|
|-- uvicorn drain (90s) --|-- buffer (30s) |
                                           |
                                        SIGKILL
```

The buffer between uvicorn's drain timeout and Docker's SIGKILL gives cleanup code time to run (close database connections, flush logs, etc.) even after uvicorn gives up on in-flight requests. Without this buffer, uvicorn and Docker race each other and cleanup may not complete.
