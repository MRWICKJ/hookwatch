package server

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type CapturedRequest struct {
	ID          string              `json:"id"`
	ReceivedAt  time.Time           `json:"received_at"`
	Method      string              `json:"method"`
	Path        string              `json:"path"`
	RemoteAddr  string              `json:"remote_addr"`
	Headers     http.Header         `json:"headers"`
	QueryParams map[string][]string `json:"query_params"`
	Body        interface{}         `json:"body"`
	SizeBytes   int64               `json:"size_bytes"`
}

type Config struct {
	Host          string
	Port          int
	MaxRequests   int
	ResponseCode  int
	ResponseBody  string
	CORSOrigins   string
}

type History struct {
	mu       sync.RWMutex
	requests []CapturedRequest
	maxSize  int
}

func NewHistory(maxSize int) *History {
	return &History{
		requests: make([]CapturedRequest, 0, maxSize),
		maxSize:  maxSize,
	}
}

func (h *History) Add(req CapturedRequest) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests = append(h.requests, req)
	if len(h.requests) > h.maxSize {
		h.requests = h.requests[1:]
	}
}

func (h *History) All() []CapturedRequest {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]CapturedRequest, len(h.requests))
	copy(out, h.requests)
	return out
}

type Hub struct {
	clients    map[*websocket.Conn]bool
	broadcast  chan CapturedRequest
	register   chan *websocket.Conn
	unregister chan *websocket.Conn
	mu         sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		clients:    make(map[*websocket.Conn]bool),
		broadcast:  make(chan CapturedRequest, 256),
		register:   make(chan *websocket.Conn),
		unregister: make(chan *websocket.Conn),
	}
}

var (
	hub       *Hub
	history   *History
	cfg       Config
	startTime time.Time
	Version   = "dev"
	Commit    = "none"
	Date      = "unknown"
)

func init() {
	flag.StringVar(&cfg.Host, "host", "localhost", "host to listen on")
	flag.IntVar(&cfg.Port, "port", 8877, "port to listen on")
	flag.IntVar(&cfg.MaxRequests, "max-requests", 500, "max requests to keep in memory")
	flag.IntVar(&cfg.ResponseCode, "response-code", 200, "HTTP status code returned to sender")
	flag.StringVar(&cfg.ResponseBody, "response-body", `{"status":"captured"}`, "JSON body returned to sender")
	flag.StringVar(&cfg.CORSOrigins, "cors-origins", "*", "allowed CORS origins (comma-separated or *)")
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close()
			}
			h.mu.Unlock()
		case req := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				if err := client.WriteJSON(req); err != nil {
					client.Close()
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if cfg.CORSOrigins == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			for _, allowed := range strings.Split(cfg.CORSOrigins, ",") {
				if strings.TrimSpace(allowed) == origin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	hub.register <- conn
	defer func() { hub.unregister <- conn }()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hub.mu.Lock()
	clientCount := len(hub.clients)
	hub.mu.Unlock()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"version":  Version,
		"uptime":   time.Since(startTime).String(),
		"requests": len(history.All()),
		"clients":  clientCount,
	})
}

func handleGetRequests(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history.All())
}

func handleClearRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	history.mu.Lock()
	history.requests = make([]CapturedRequest, 0, cfg.MaxRequests)
	history.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"cleared"}`))
}

func captureRequest(w http.ResponseWriter, r *http.Request) {
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusInternalServerError)
			return
		}
	}

	var parsedBody interface{}
	if err := json.Unmarshal(bodyBytes, &parsedBody); err != nil {
		parsedBody = string(bodyBytes)
	}

	captured := CapturedRequest{
		ID:          uuid.New().String(),
		ReceivedAt:  time.Now(),
		Method:      r.Method,
		Path:        r.URL.Path,
		RemoteAddr:  r.RemoteAddr,
		Headers:     r.Header.Clone(),
		QueryParams: r.URL.Query(),
		Body:        parsedBody,
		SizeBytes:   int64(len(bodyBytes)),
	}

	history.Add(captured)
	hub.broadcast <- captured

	log.Printf("[req] %s %s (%d bytes)", r.Method, r.URL.Path, len(bodyBytes))
	w.WriteHeader(cfg.ResponseCode)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(cfg.ResponseBody))
}

func Start(staticFS fs.FS) {
	flag.Parse()
	startTime = time.Now()

	hub = NewHub()
	history = NewHistory(cfg.MaxRequests)
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/ws" {
			handleWebSocket(w, r)
			return
		}
		if p == "/health" {
			handleHealth(w, r)
			return
		}
		if strings.HasPrefix(p, "/api/") {
			switch p {
			case "/api/requests":
				handleGetRequests(w, r)
			case "/api/clear":
				handleClearRequests(w, r)
			default:
				http.NotFound(w, r)
			}
			return
		}
		if r.Method == "GET" && staticFS != nil {
			p = strings.TrimPrefix(p, "/")
			if p == "" {
				p = "index.html"
			}
			if _, err := fs.Stat(staticFS, p); err == nil {
				http.FileServer(http.FS(staticFS)).ServeHTTP(w, r)
				return
			}
		}
		captureRequest(w, r)
	})

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	fmt.Print(`  _   _           _      _       _     
 | | | | ___   __| | ___| |_   / \   
 | |_| |/ _ \ / _  |/ _ \ __| / _ \  
 |  _  | (_) | (_| |  __/ |_ / ___ \ 
 |_| |_|\___/ \__,_|\___|\__/_/   \_\
`)
	fmt.Printf("  HookWatch v%-6s   http://%s\n", Version, addr)
	fmt.Printf("  Dashboard        http://%s\n", addr)
	fmt.Printf("  Send webhooks    http://%s/your-path\n", addr)
	fmt.Println()

	server := &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(mux),
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		fmt.Println("\n  shutting down...")
		server.Close()
	}()

	log.Printf("started on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("error: %v", err)
	}
}
