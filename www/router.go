package www

import (
	"encoding/json"
	"html/template"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"warlink/config"
	"warlink/kafka"
	"warlink/mqtt"
	"warlink/plcman"
	"warlink/tagpack"
	"warlink/trigger"
	"warlink/valkey"
)

// Managers provides access to shared backend managers.
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

// Handlers holds all HTTP handlers for the web UI.
type Handlers struct {
	cfg      *config.WebUIConfig
	managers Managers
	sessions *sessionStore
	tmpl     *template.Template
	eventHub *EventHub
}

// newHandlers creates a new handlers instance.
func newHandlers(cfg *config.WebUIConfig, managers Managers) *Handlers {
	h := &Handlers{
		cfg:      cfg,
		managers: managers,
		sessions: newSessionStore(cfg.SessionSecret),
		eventHub: newEventHub(),
	}

	// Parse templates
	h.tmpl = template.Must(template.New("").Funcs(template.FuncMap{
		"isAdmin": isAdmin,
		"lower":   strings.ToLower,
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"countTags": func(sections []RepublisherSection) int {
			count := 0
			for _, s := range sections {
				count += len(s.Tags)
			}
			return count
		},
	}).ParseFS(templatesFS, "templates/*.html", "templates/partials/*.html"))

	// Set up event listeners for SSE broadcasting
	h.setupEventListeners()

	return h
}

// NewRouter creates the web UI router.
func NewRouter(cfg *config.WebUIConfig, managers Managers) chi.Router {
	h := newHandlers(cfg, managers)

	r := chi.NewRouter()

	// Static files (public)
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Login/logout (public)
	r.Get("/login", h.handleLoginPage)
	r.Post("/login", h.handleLoginSubmit)
	r.Post("/logout", h.handleLogout)

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(h.authMiddleware)

		// SSE endpoint for real-time updates
		r.Get("/events/republisher", h.handleSSERepublisher)

		// Pages
		r.Get("/", h.handleDashboard)
		r.Get("/plcs", h.handlePLCsPage)
		r.Get("/republisher", h.handleRepublisherPage)
		r.Get("/tagpacks", h.handleTagPacksPage)
		r.Get("/triggers", h.handleTriggersPage)
		r.Get("/rest", h.handleRESTPage)
		r.Get("/mqtt", h.handleMQTTPage)
		r.Get("/valkey", h.handleValkeyPage)
		r.Get("/kafka", h.handleKafkaPage)
		r.Get("/debug", h.handleDebugPage)

		// htmx partials (polling)
		r.Get("/htmx/plcs", h.handlePLCsPartial)
		r.Get("/htmx/republisher", h.handleRepublisherPartial)
		r.Get("/htmx/mqtt", h.handleMQTTPartial)
		r.Get("/htmx/valkey", h.handleValkeyPartial)
		r.Get("/htmx/kafka", h.handleKafkaPartial)
		r.Get("/htmx/tagpacks", h.handleTagPacksPartial)
		r.Get("/htmx/triggers", h.handleTriggersPartial)
		r.Get("/htmx/debug", h.handleDebugPartial)

		// Actions (admin only)
		r.Group(func(r chi.Router) {
			r.Use(h.adminOnlyMiddleware)

			// PLC discovery
			r.Get("/htmx/discover", h.handleDiscoverPLCs)

			// PLC actions
			r.Post("/htmx/plcs", h.handlePLCCreate)
			r.Post("/htmx/plcs/{name}/connect", h.handlePLCConnect)
			r.Post("/htmx/plcs/{name}/disconnect", h.handlePLCDisconnect)
			r.Get("/htmx/plcs/{name}", h.handlePLCGet)
			r.Put("/htmx/plcs/{name}", h.handlePLCUpdate)
			r.Delete("/htmx/plcs/{name}", h.handlePLCDelete)

			// MQTT actions
			r.Post("/htmx/mqtt", h.handleMQTTCreate)
			r.Get("/htmx/mqtt/{name}", h.handleMQTTGet)
			r.Put("/htmx/mqtt/{name}", h.handleMQTTUpdate)
			r.Delete("/htmx/mqtt/{name}", h.handleMQTTDelete)
			r.Post("/htmx/mqtt/{name}/start", h.handleMQTTStart)
			r.Post("/htmx/mqtt/{name}/stop", h.handleMQTTStop)

			// Valkey actions
			r.Post("/htmx/valkey", h.handleValkeyCreate)
			r.Get("/htmx/valkey/{name}", h.handleValkeyGet)
			r.Put("/htmx/valkey/{name}", h.handleValkeyUpdate)
			r.Delete("/htmx/valkey/{name}", h.handleValkeyDelete)
			r.Post("/htmx/valkey/{name}/start", h.handleValkeyStart)
			r.Post("/htmx/valkey/{name}/stop", h.handleValkeyStop)

			// Kafka actions
			r.Post("/htmx/kafka", h.handleKafkaCreate)
			r.Get("/htmx/kafka/{name}", h.handleKafkaGet)
			r.Put("/htmx/kafka/{name}", h.handleKafkaUpdate)
			r.Delete("/htmx/kafka/{name}", h.handleKafkaDelete)
			r.Post("/htmx/kafka/{name}/connect", h.handleKafkaConnect)
			r.Post("/htmx/kafka/{name}/disconnect", h.handleKafkaDisconnect)

			// TagPack actions
			r.Post("/htmx/tagpacks", h.handleTagPackCreate)
			r.Get("/htmx/tagpacks/{name}", h.handleTagPackGet)
			r.Put("/htmx/tagpacks/{name}", h.handleTagPackUpdate)
			r.Delete("/htmx/tagpacks/{name}", h.handleTagPackDelete)
			r.Patch("/htmx/tagpacks/{name}", h.handleTagPackToggle)
			r.Post("/htmx/tagpacks/{name}/service/{service}", h.handleTagPackServiceToggle)
			r.Post("/htmx/tagpacks/{name}/members", h.handleTagPackAddMember)
			r.Delete("/htmx/tagpacks/{name}/members/{index}", h.handleTagPackRemoveMember)
			r.Patch("/htmx/tagpacks/{name}/members/{index}", h.handleTagPackToggleMemberIgnore)

			// Trigger actions
			r.Post("/htmx/triggers", h.handleTriggerCreate)
			r.Get("/htmx/triggers/{name}", h.handleTriggerGet)
			r.Put("/htmx/triggers/{name}", h.handleTriggerUpdate)
			r.Delete("/htmx/triggers/{name}", h.handleTriggerDelete)
			r.Post("/htmx/triggers/{name}/start", h.handleTriggerStart)
			r.Post("/htmx/triggers/{name}/stop", h.handleTriggerStop)
			r.Post("/htmx/triggers/{name}/test", h.handleTriggerTestFire)
			r.Post("/htmx/triggers/{name}/tags", h.handleTriggerAddTag)
			r.Delete("/htmx/triggers/{name}/tags/{index}", h.handleTriggerRemoveTag)

			// Tag picker data
			r.Get("/htmx/available-tags", h.handleAvailableTags)
			r.Get("/htmx/plc-tags/{plc}", h.handlePLCTags)

			// Debug actions
			r.Post("/htmx/debug/clear", h.handleDebugClear)

			// Tag actions
			r.Get("/htmx/tags/{plc}/{tag}", h.handleTagRead)
			r.Patch("/htmx/tags/{plc}/{tag}", h.handleTagUpdate)
			r.Put("/htmx/tags/{plc}/{tag}", h.handleTagPut)
			r.Delete("/htmx/tags/{plc}/{tag}", h.handleTagDelete)
			r.Post("/htmx/tags/{plc}/{tag}/write", h.handleTagWrite)

			// PLC type info
			r.Get("/htmx/plcs/{plc}/types", h.handlePLCTypeNames)
		})

		// User management (admin only)
		r.Route("/users", func(r chi.Router) {
			r.Use(h.adminOnlyMiddleware)
			r.Get("/", h.handleUsersPage)
			r.Get("/htmx", h.handleUsersPartial)
			r.Post("/", h.handleUserCreate)
			r.Put("/{username}", h.handleUserUpdate)
			r.Delete("/{username}", h.handleUserDelete)
		})
	})

	return r
}

// authMiddleware checks if the user is authenticated.
func (h *Handlers) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, _, ok := h.sessions.getUser(r)
		if !ok || username == "" {
			// Check if this is an htmx request
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Verify user still exists in config
		if h.managers.GetConfig().FindWebUser(username) == nil {
			h.sessions.clear(w, r)
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// adminOnlyMiddleware checks if the user has admin role.
func (h *Handlers) adminOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, role, ok := h.sessions.getUser(r)
		if !ok || !isAdmin(role) {
			if r.Header.Get("HX-Request") == "true" {
				http.Error(w, "Forbidden: Admin access required", http.StatusForbidden)
				return
			}
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// renderTemplate renders a template with common data.
func (h *Handlers) renderTemplate(w http.ResponseWriter, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// getUserInfo returns the current user info for templates.
func (h *Handlers) getUserInfo(r *http.Request) map[string]interface{} {
	username, role, _ := h.sessions.getUser(r)
	return map[string]interface{}{
		"Username": username,
		"Role":     role,
		"IsAdmin":  isAdmin(role),
	}
}
