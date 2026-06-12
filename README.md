# HookWatch

A production-grade local request inspector. Captures HTTP requests in real-time and displays them in a WebSocket-powered dashboard.

- **Zero-config intercept** — send traffic to `localhost:8877`, see it instantly
- **Live WebSocket stream** — no polling, no refresh
- **Request history** — last N requests kept in memory (configurable)
- **Search & filter** — filter by method, search by path
- **Copy as cURL** — replay any captured request instantly
- **Single binary deploy** — Go backend + React frontend bundled together
- **Docker ready** — one command to run everything

## Quick Start

### Docker (recommended)

```bash
docker compose up --build
```

Open [http://localhost:8877](http://localhost:8877).

### Manual

#### 1. Start the server

```bash
cd server
go run main.go
```

#### 2. Start the frontend

```bash
cd frontend
npm install
npm run dev
```

Open [http://localhost:5173](http://localhost:5173).

#### 3. Send a test request

```bash
curl -X POST http://localhost:8877/hello \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello HookWatch!"}'
```

## Usage

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--port` | `8877` | Port to listen on |
| `--host` | `0.0.0.0` | Host to bind to |
| `--max-requests` | `500` | Max requests kept in memory |
| `--response-code` | `200` | HTTP status returned to the sender |
| `--response-body` | `{"status":"captured"}` | JSON body returned to the sender |
| `--cors-origins` | `*` | Allowed CORS origins (comma-separated) |
| `--static-dir` | `""` | Directory with frontend static files to serve |

### Single Binary Deployment

Serve both the API and frontend from one binary:

```bash
cd frontend && npm run build
cd ../server && go build -o hookwatch .
./hookwatch --port 8877 --static-dir ../frontend/dist
```

Then visit [http://localhost:8877](http://localhost:8877).

### Endpoints

| Path | Description |
|------|-------------|
| `/*` | Capture any HTTP request |
| `/ws` | WebSocket stream of captured requests |
| `/health` | Health check with stats |
| `/api/requests` | GET all captured requests (JSON) |
| `/api/clear` | POST to clear all captured requests |

### Docker

```bash
# Build and run
docker compose up --build

# Or build the image directly
docker build -t hookwatch .
docker run -p 8877:8877 hookwatch
```

## Architecture

```
                  curl / webhook
                        │
                        ▼
┌──────────────────────────────────┐
│         Go Server (:8877)        │
│  ┌────────────┐  ┌────────────┐  │
│  │ Capture    │──│ WebSocket  │  │
│  │ & Store    │  │ Broadcast  │  │
│  └────────────┘  └─────┬──────┘  │
│  ┌────────────┐        │         │
│  │ REST API   │        │         │
│  └────────────┘        │         │
└────────────────────────┼─────────┘
                         │ ws://
                         ▼
┌──────────────────────────────────┐
│      React Dashboard             │
│  Search · Filter · cURL · Live   │
└──────────────────────────────────┘
```

- **Server** (`server/main.go`): Captures all inbound HTTP requests, stores them in a configurable ring buffer, and broadcasts to all connected WebSocket clients. Serves the React frontend as static files when `--static-dir` is set.
- **Frontend** (`frontend/src/App.tsx`): React + TypeScript + Tailwind CSS. Connects to the WebSocket stream, displays requests in a split-panel inspector with search, method filtering, copy-as-cURL, pause/resume, and auto-reconnect.
