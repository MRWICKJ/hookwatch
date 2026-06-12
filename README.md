# HookWatch

A local request inspector. Watch HTTP requests in real-time.

```bash
docker compose up --build
# or: go run .
```

Open http://localhost:8877

```bash
curl -X POST http://localhost:8877/test -d '{"hello":"world"}'
```

## One command

| Method | Command |
|--------|---------|
| Docker | `docker compose up --build` |
| Go | `go run .` |
| Binary | `go build -o hookwatch . && ./hookwatch` |

## CLI flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8877` | Port to listen on |
| `--max-requests` | `500` | Requests stored in memory |
| `--response-code` | `200` | HTTP status returned to sender |

## Endpoints

| Path | Description |
|------|-------------|
| `/` | Dashboard UI |
| `/*` | Capture any HTTP request |
| `/ws` | WebSocket stream |
| `/health` | Health check |
| `/api/requests` | GET captured requests |
| `/api/clear` | POST to clear |

## Dev mode

```bash
# Terminal 1 - Go server
go run .

# Terminal 2 - frontend with hot reload
cd frontend && npm install && npm run dev
```
