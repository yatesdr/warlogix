package www

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/gorilla/sessions"
	"golang.org/x/crypto/bcrypt"

	"warlink/config"
)

const (
	sessionName    = "warlink_session"
	sessionUserKey = "username"
	sessionRoleKey = "role"
)

// sessionStore is the session store for the web UI.
type sessionStore struct {
	store *sessions.CookieStore
}

// newSessionStore creates a new session store with the given secret.
func newSessionStore(secret string) *sessionStore {
	// Decode secret or generate one if empty
	var key []byte
	if secret != "" {
		key, _ = base64.StdEncoding.DecodeString(secret)
	}
	if len(key) < 32 {
		key = make([]byte, 32)
		rand.Read(key)
	}

	store := sessions.NewCookieStore(key)
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	return &sessionStore{store: store}
}

// get retrieves the session from the request.
// Gorilla's CookieStore.Get may return a decode error for stale cookies
// (e.g. after session secret rotation), but always returns a usable session.
// We ignore the error so stale cookies don't block login/logout.
func (s *sessionStore) get(r *http.Request) *sessions.Session {
	session, _ := s.store.Get(r, sessionName)
	return session
}

// getUser returns the username and role from the session.
func (s *sessionStore) getUser(r *http.Request) (username, role string, ok bool) {
	session := s.get(r)

	user, uok := session.Values[sessionUserKey].(string)
	role, rok := session.Values[sessionRoleKey].(string)
	if !uok || !rok || user == "" {
		return "", "", false
	}

	return user, role, true
}

// setUser stores the username and role in the session.
func (s *sessionStore) setUser(w http.ResponseWriter, r *http.Request, username, role string) error {
	session := s.get(r)
	session.Values[sessionUserKey] = username
	session.Values[sessionRoleKey] = role
	return session.Save(r, w)
}

// clear removes the user from the session.
func (s *sessionStore) clear(w http.ResponseWriter, r *http.Request) error {
	session := s.get(r)
	delete(session.Values, sessionUserKey)
	delete(session.Values, sessionRoleKey)
	session.Options.MaxAge = -1
	return session.Save(r, w)
}

// checkPassword verifies a password against a bcrypt hash.
func checkPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

// hashPassword generates a bcrypt hash of the password.
func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// isAdmin returns true if the role is admin.
func isAdmin(role string) bool {
	return role == config.RoleAdmin
}

