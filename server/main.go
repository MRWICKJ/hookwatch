package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	StaticDir     string
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
	hub     *Hub
	history *History
	config  Config
)

func init() {
	flag.StringVar(&config.Host, "host", "0.0.0.0", "host to listen on")
	flag.IntVar(&config.Port, "port", 8877, "port to listen on")
	flag.IntVar(&config.MaxRequests, "max-requests", 500, "max requests to keep in memory")
	flag.IntVar(&config.ResponseCode, "response-code", 200, "HTTP status code to return to webhook sender")
	flag.StringVar(&config.ResponseBody, "response-body", `{"status":"captured"}`, "JSON body to return to webhook sender")
	flag.StringVar(&config.CORSOrigins, "cors-origins", "*", "allowed CORS origins (comma-separated or *)")
	flag.StringVar(&config.StaticDir, "static-dir", "", "serve static frontend files from this directory")
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Println("[ws] client connected")
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				client.Close()
			}
			h.mu.Unlock()
			log.Println("[ws] client disconnected")
		case req := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				if err := client.WriteJSON(req); err != nil {
					log.Printf("[ws] write error: %v", err)
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
		if config.CORSOrigins == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			for _, allowed := range splitOrigins(config.CORSOrigins) {
				if allowed == origin {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func splitOrigins(s string) []string {
	if s == "" {
		return nil
	}
	var origins []string
	var buf []byte
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			if len(buf) > 0 {
				origins = append(origins, string(buf))
				buf = nil
			}
		} else {
			buf = append(buf, s[i])
		}
	}
	if len(buf) > 0 {
		origins = append(origins, string(buf))
	}
	return origins
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade failed: %v", err)
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
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"uptime":    time.Since(startTime).String(),
		"requests":  len(history.All()),
		"clients":   func() int { hub.mu.Lock(); defer hub.mu.Unlock(); return len(hub.clients) }(),
		"max":       config.MaxRequests,
	})
}

func handleGetRequests(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history.All())
}

func handleClearRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	history.mu.Lock()
	history.requests = make([]CapturedRequest, 0, config.MaxRequests)
	history.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"cleared"}`))
}

var (
	startTime time.Time
	version   = "dev"
	commit    = "none"
	date      = "unknown"
)

func catchAllRequests(w http.ResponseWriter, r *http.Request) {
	// Serve static frontend if configured
	if config.StaticDir != "" && strings.HasPrefix(r.URL.Path, "/api") == false && r.URL.Path != "/ws" {
		filePath := filepath.Join(config.StaticDir, r.URL.Path)
		if r.URL.Path == "/" || r.URL.Path == "" {
			filePath = filepath.Join(config.StaticDir, "index.html")
		}
		if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
			http.ServeFile(w, r, filePath)
			return
		}
		// SPA fallback
		indexPath := filepath.Join(config.StaticDir, "index.html")
		if _, err := os.Stat(indexPath); err == nil {
			http.ServeFile(w, r, indexPath)
			return
		}
	}

	switch {
	case r.URL.Path == "/ws":
		handleWebSocket(w, r)
		return
	case r.URL.Path == "/health" || r.URL.Path == "/api/health":
		handleHealth(w, r)
		return
	case r.URL.Path == "/api/requests":
		handleGetRequests(w, r)
		return
	case r.URL.Path == "/api/clear":
		handleClearRequests(w, r)
		return
	}

	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusInternalServerError)
			return
		}
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

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

	log.Printf("[req] %s %s from %s (%d bytes)", r.Method, r.URL.Path, r.RemoteAddr, len(bodyBytes))

	w.WriteHeader(config.ResponseCode)
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(config.ResponseBody))
}

func main() {
	flag.Parse()
	startTime = time.Now()

	hub = NewHub()
	history = NewHistory(config.MaxRequests)

	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/", catchAllRequests)

	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)

	log.SetFlags(log.Ltime | log.Lmsgprefix)
	log.SetPrefix("[hookwatch] ")

	fmt.Println(`
  _   _           _      _       _     
 | | | | ___   __| | ___| |_   / \   
 | |_| |/ _ \ / _  |/ _ \ __| / _ \  
 |  _  | (_) | (_| |  __/ |_ / ___ \ 
 |_| |_|\___/ \__,_|\___|\__/_/   \_\
                                     
`)

	fmt.Printf("  HookWatch v%s     http://%s\n", version, addr)
	fmt.Printf("  WebSocket          ws://%s/ws\n", addr)
	fmt.Printf("  Health             http://%s/health\n", addr)
	fmt.Printf("  Max Requests:      %d\n", config.MaxRequests)
	fmt.Printf("  Build:             %s (%s)\n", commit, date)
	fmt.Println()

	server := &http.Server{
		Addr:    addr,
		Handler: corsMiddleware(mux),
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		fmt.Println("\n  Shutting down gracefully...")
		server.Close()
	}()

	log.Printf("server started on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}
