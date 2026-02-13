package www

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"warlink/config"
)

// handleUsersPage renders the user management page.
func (h *Handlers) handleUsersPage(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Page"] = "users"
	data["Users"] = h.getUsersData()
	h.renderTemplate(w, "users.html", data)
}

// handleUsersPartial returns the users table partial.
func (h *Handlers) handleUsersPartial(w http.ResponseWriter, r *http.Request) {
	data := h.getUserInfo(r)
	data["Users"] = h.getUsersData()
	h.renderTemplate(w, "users_table.html", data)
}

// UserData holds user display data.
type UserData struct {
	Username string
	Role     string
	IsAdmin  bool
}

func (h *Handlers) getUsersData() []UserData {
	cfg := h.managers.GetConfig()
	users := cfg.Web.UI.Users
	result := make([]UserData, 0, len(users))

	for _, u := range users {
		result = append(result, UserData{
			Username: u.Username,
			Role:     u.Role,
			IsAdmin:  isAdmin(u.Role),
		})
	}

	return result
}

// UserRequest represents a user create/update request.
type UserRequest struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	Role     string `json:"role"`
}

// handleUserCreate creates a new user.
func (h *Handlers) handleUserCreate(w http.ResponseWriter, r *http.Request) {
	var req UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		http.Error(w, "Username is required", http.StatusBadRequest)
		return
	}
	if req.Password == "" {
		http.Error(w, "Password is required", http.StatusBadRequest)
		return
	}
	if req.Role != config.RoleAdmin && req.Role != config.RoleViewer {
		http.Error(w, "Role must be 'admin' or 'viewer'", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()

	// Check if user already exists
	if cfg.FindWebUser(req.Username) != nil {
		http.Error(w, "User already exists", http.StatusConflict)
		return
	}

	// Hash password
	hash, err := hashPassword(req.Password)
	if err != nil {
		http.Error(w, "Failed to hash password: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Add user
	cfg.Lock()
	cfg.AddWebUser(config.WebUser{
		Username:     req.Username,
		PasswordHash: hash,
		Role:         req.Role,
	})

	// Save config
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated partial
	h.handleUsersPartial(w, r)
}

// handleUserUpdate updates an existing user.
func (h *Handlers) handleUserUpdate(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	username, _ = url.PathUnescape(username)

	var req UserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Role != config.RoleAdmin && req.Role != config.RoleViewer {
		http.Error(w, "Role must be 'admin' or 'viewer'", http.StatusBadRequest)
		return
	}

	cfg := h.managers.GetConfig()

	// Find existing user
	user := cfg.FindWebUser(username)
	if user == nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Update fields
	updated := config.WebUser{
		Username:     username,
		PasswordHash: user.PasswordHash,
		Role:         req.Role,
	}

	// Update password if provided
	if req.Password != "" {
		hash, err := hashPassword(req.Password)
		if err != nil {
			http.Error(w, "Failed to hash password: "+err.Error(), http.StatusInternalServerError)
			return
		}
		updated.PasswordHash = hash
	}

	cfg.Lock()
	cfg.UpdateWebUser(username, updated)

	// Save config
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated partial
	h.handleUsersPartial(w, r)
}

// handleUserDelete deletes a user.
func (h *Handlers) handleUserDelete(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	username, _ = url.PathUnescape(username)

	cfg := h.managers.GetConfig()

	// Don't allow deleting yourself
	currentUser, _, _ := h.sessions.getUser(r)
	if currentUser == username {
		http.Error(w, "Cannot delete your own account", http.StatusBadRequest)
		return
	}

	// Check if this is the last admin
	user := cfg.FindWebUser(username)
	if user != nil && isAdmin(user.Role) {
		adminCount := 0
		for _, u := range cfg.Web.UI.Users {
			if isAdmin(u.Role) {
				adminCount++
			}
		}
		if adminCount <= 1 {
			http.Error(w, "Cannot delete the last admin user", http.StatusBadRequest)
			return
		}
	}

	// Remove user
	cfg.Lock()
	if !cfg.RemoveWebUser(username) {
		cfg.Unlock()
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}

	// Save config
	if err := cfg.UnlockAndSave(h.managers.GetConfigPath()); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Return updated partial
	h.handleUsersPartial(w, r)
}
