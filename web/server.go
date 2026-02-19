// Package web provides a unified HTTP server for the REST API and browser UI.
package web

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"warlink/api"
	"warlink/config"
	"warlink/engine"
	"github.com/yatesdr/plcio/logging"
	"warlink/www"
)

// Server is the unified HTTP server for REST API and browser UI.
type Server struct {
	config   *config.WebConfig
	managers engine.Managers
	engine   *engine.Engine
	server   *http.Server
	router   chi.Router
	running  bool
	mu       sync.RWMutex

	// Cleanup functions for SSE event hubs and listeners
	apiCleanup func()
	uiCleanup  func()

	// Unsecured deadline timer
	deadlineTimer *time.Timer
	deadlineMu    sync.Mutex
}

// NewServer creates a new unified web server.
func NewServer(cfg *config.WebConfig, managers engine.Managers) *Server {
	s := &Server{
		config:   cfg,
		managers: managers,
	}

	// If managers is an *engine.Engine, capture it for mutation operations.
	if eng, ok := managers.(*engine.Engine); ok {
		s.engine = eng
	}

	s.setupRoutes()
	return s
}

// setupRoutes configures the chi router with all routes.
func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RealIP)
	r.Use(middleware.Compress(5))

	// CORS for API
	r.Use(corsMiddleware)

	// Mount REST API at /api
	if s.config.API.Enabled {
		apiRouter, apiCleanup := api.NewRouter(s.managers)
		s.apiCleanup = apiCleanup
		r.Mount("/api", apiRouter)
	}

	// Mount Web UI at root
	if s.config.UI.Enabled {
		uiRouter, cleanup := www.NewRouter(&s.config.UI, s.managers, s.engine, s)
		s.uiCleanup = cleanup
		r.Mount("/", uiRouter)
	}

	s.router = r
}

// debugLogWriter adapts logging.DebugLog to an io.Writer for use with log.Logger.
type debugLogWriter string

func (tag debugLogWriter) Write(p []byte) (n int, err error) {
	logging.DebugLog(string(tag), "%s", string(p))
	return len(p), nil
}

// Verify debugLogWriter implements io.Writer.
var _ io.Writer = debugLogWriter("")

// corsMiddleware adds CORS headers for API access.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Start begins the HTTP server.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return nil
	}

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.server = &http.Server{
		Addr:              addr,
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(debugLogWriter("browser"), "", 0),
	}

	go func() {
		if err := s.server.ListenAndServe(); err != http.ErrServerClosed {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}
	}()

	s.running = true
	return nil
}

// Stop halts the HTTP server gracefully.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.server == nil {
		return nil
	}

	// Stop API SSE hub and listeners
	if s.apiCleanup != nil {
		s.apiCleanup()
		s.apiCleanup = nil
	}

	// Stop UI SSE event hub and other resources
	if s.uiCleanup != nil {
		s.uiCleanup()
		s.uiCleanup = nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.server.Shutdown(ctx)
	s.running = false
	s.server = nil
	return err
}

// IsRunning returns whether the server is currently running.
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// Address returns the server address.
func (s *Server) Address() string {
	return fmt.Sprintf("http://%s:%d", s.config.Host, s.config.Port)
}

// Reload reconfigures routes with updated config.
// Call this after config changes that affect enabled state.
func (s *Server) Reload(cfg *config.WebConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up old SSE hubs/listeners before rebuilding routes
	if s.apiCleanup != nil {
		s.apiCleanup()
		s.apiCleanup = nil
	}
	if s.uiCleanup != nil {
		s.uiCleanup()
		s.uiCleanup = nil
	}

	s.config = cfg
	s.setupRoutes()
	if s.server != nil {
		s.server.Handler = s.router
	}
}

// SetUnsecuredDeadline starts a timer that stops the server after the given duration.
// The onExpiry callback is called after the server is stopped.
func (s *Server) SetUnsecuredDeadline(d time.Duration, onExpiry func()) {
	s.deadlineMu.Lock()
	defer s.deadlineMu.Unlock()

	if s.deadlineTimer != nil {
		s.deadlineTimer.Stop()
	}

	s.deadlineTimer = time.AfterFunc(d, func() {
		s.Stop()
		if onExpiry != nil {
			onExpiry()
		}
	})
}

// ClearUnsecuredDeadline cancels the unsecured deadline timer if running.
func (s *Server) ClearUnsecuredDeadline() {
	s.deadlineMu.Lock()
	defer s.deadlineMu.Unlock()

	if s.deadlineTimer != nil {
		s.deadlineTimer.Stop()
		s.deadlineTimer = nil
	}
}
