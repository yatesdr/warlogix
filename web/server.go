// Package web provides a unified HTTP server for the REST API and browser UI.
package web

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"warlink/api"
	"warlink/config"
	"warlink/kafka"
	"warlink/mqtt"
	"warlink/plcman"
	"warlink/tagpack"
	"warlink/trigger"
	"warlink/valkey"
	"warlink/www"
)

// Managers provides access to shared backend managers.
// This interface matches the methods available on ssh.SharedManagers.
type Managers interface {
	GetConfig() *config.Config
	GetConfigPath() string
	GetPLCMan() *plcman.Manager
	GetMQTTMgr() *mqtt.Manager
	GetValkeyMgr() *valkey.Manager
	GetKafkaMgr() *kafka.Manager
	GetTriggerMgr() *trigger.Manager
	GetPackMgr() *tagpack.Manager
}

// Server is the unified HTTP server for REST API and browser UI.
type Server struct {
	config   *config.WebConfig
	managers Managers
	server   *http.Server
	router   chi.Router
	running  bool
	mu       sync.RWMutex
}

// NewServer creates a new unified web server.
func NewServer(cfg *config.WebConfig, managers Managers) *Server {
	s := &Server{
		config:   cfg,
		managers: managers,
	}
	s.setupRoutes()
	return s
}

// setupRoutes configures the chi router with all routes.
func (s *Server) setupRoutes() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// CORS for API
	r.Use(corsMiddleware)

	// Mount REST API at /api
	if s.config.API.Enabled {
		r.Mount("/api", api.NewRouter(s.managers))
	}

	// Mount Web UI at root
	if s.config.UI.Enabled && len(s.config.UI.Users) > 0 {
		r.Mount("/", www.NewRouter(&s.config.UI, s.managers))
	}

	s.router = r
}

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
	s.config = cfg
	s.setupRoutes()
	if s.server != nil {
		s.server.Handler = s.router
	}
}
