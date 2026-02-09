package ssh

import (
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"warlink/tui"
)

// Server handles SSH connections and multiplexes them to a shared PTY.
type Server struct {
	config     *Config
	server     *ssh.Server
	listener   net.Listener
	ptyMaster  *os.File
	ptySlave   *os.File
	sessions   map[ssh.Session]struct{}
	sessionsMu sync.RWMutex
	running    bool
	mu         sync.Mutex
	stopChan   chan struct{}

	// Callbacks
	onSessionConnect    func(remoteAddr string)
	onSessionDisconnect func(remoteAddr string)
}

// Config holds SSH server configuration.
type Config struct {
	Port           int
	Password       string
	AuthorizedKeys string
}

// NewServer creates a new SSH server.
func NewServer(config *Config) *Server {
	return &Server{
		config:   config,
		sessions: make(map[ssh.Session]struct{}),
		stopChan: make(chan struct{}),
	}
}

// SetOnSessionConnect sets a callback for when a session connects.
func (s *Server) SetOnSessionConnect(fn func(remoteAddr string)) {
	s.onSessionConnect = fn
}

// SetOnSessionDisconnect sets a callback for when a session disconnects.
func (s *Server) SetOnSessionDisconnect(fn func(remoteAddr string)) {
	s.onSessionDisconnect = fn
}

// GetPTYSlave returns the slave end of the PTY for the TUI to use.
func (s *Server) GetPTYSlave() *os.File {
	return s.ptySlave
}

// GetPTYMaster returns the master end of the PTY.
func (s *Server) GetPTYMaster() *os.File {
	return s.ptyMaster
}

// Start starts the SSH server.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}

	// Create PTY pair
	master, slave, err := pty.Open()
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to create PTY: %w", err)
	}
	s.ptyMaster = master
	s.ptySlave = slave

	// Get or create host key
	hostKey, err := GetOrCreateHostKey()
	if err != nil {
		master.Close()
		slave.Close()
		s.mu.Unlock()
		return fmt.Errorf("failed to get host key: %w", err)
	}

	// Create SSH server
	s.server = &ssh.Server{
		Addr:        fmt.Sprintf(":%d", s.config.Port),
		HostSigners: []ssh.Signer{hostKey},
		Handler:     s.sessionHandler,
	}

	// Configure authentication
	if s.config.Password != "" {
		s.server.PasswordHandler = PasswordHandler(s.config.Password)
	}
	if s.config.AuthorizedKeys != "" {
		s.server.PublicKeyHandler = PublicKeyHandler(s.config.AuthorizedKeys)
	}

	// At least one auth method must be configured
	if s.server.PasswordHandler == nil && s.server.PublicKeyHandler == nil {
		master.Close()
		slave.Close()
		s.mu.Unlock()
		return fmt.Errorf("no authentication method configured")
	}

	// Start listener
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		master.Close()
		slave.Close()
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", s.server.Addr, err)
	}
	s.listener = listener
	s.running = true
	s.stopChan = make(chan struct{})
	s.mu.Unlock()

	tui.DebugLogSSH("Server started on port %d", s.config.Port)

	// Start PTY output multiplexer
	go s.multiplexPTYOutput()

	// Serve in background
	go func() {
		if err := s.server.Serve(listener); err != nil && err != ssh.ErrServerClosed {
			tui.DebugLogSSH("Server error: %v", err)
		}
	}()

	return nil
}

// Stop stops the SSH server gracefully.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}

	s.running = false

	// Close stopChan to signal goroutines
	select {
	case <-s.stopChan:
		// Already closed
	default:
		close(s.stopChan)
	}
	s.mu.Unlock()

	// Close all sessions in goroutines (don't block on slow closes)
	s.sessionsMu.RLock()
	for session := range s.sessions {
		go session.Close()
	}
	s.sessionsMu.RUnlock()

	// Close listener
	if s.listener != nil {
		s.listener.Close()
	}

	// Close PTY
	if s.ptyMaster != nil {
		s.ptyMaster.Close()
	}
	if s.ptySlave != nil {
		s.ptySlave.Close()
	}

	return nil
}

// IsRunning returns whether the server is running.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// SessionCount returns the number of active sessions.
func (s *Server) SessionCount() int {
	s.sessionsMu.RLock()
	defer s.sessionsMu.RUnlock()
	return len(s.sessions)
}

// sessionHandler handles new SSH sessions.
func (s *Server) sessionHandler(session ssh.Session) {
	remoteAddr := session.RemoteAddr().String()
	tui.DebugLogSSH("Session connected from %s", remoteAddr)

	// Request PTY from client
	ptyReq, winCh, isPty := session.Pty()
	if !isPty {
		io.WriteString(session, "PTY required\n")
		return
	}

	// Set initial PTY size
	if err := pty.Setsize(s.ptyMaster, &pty.Winsize{
		Rows: uint16(ptyReq.Window.Height),
		Cols: uint16(ptyReq.Window.Width),
	}); err != nil {
		tui.DebugLogSSH("Failed to set PTY size: %v", err)
	}

	// Register session
	s.sessionsMu.Lock()
	s.sessions[session] = struct{}{}
	s.sessionsMu.Unlock()

	if s.onSessionConnect != nil {
		s.onSessionConnect(remoteAddr)
	}

	// Handle window size changes
	go func() {
		for win := range winCh {
			if err := pty.Setsize(s.ptyMaster, &pty.Winsize{
				Rows: uint16(win.Height),
				Cols: uint16(win.Width),
			}); err != nil {
				tui.DebugLogSSH("Failed to resize PTY: %v", err)
			}
		}
	}()

	// Forward input from SSH session to PTY master
	go func() {
		io.Copy(s.ptyMaster, session)
	}()

	// Wait for session to end
	<-session.Context().Done()

	// Unregister session
	s.sessionsMu.Lock()
	delete(s.sessions, session)
	s.sessionsMu.Unlock()

	if s.onSessionDisconnect != nil {
		s.onSessionDisconnect(remoteAddr)
	}

	tui.DebugLogSSH("Session disconnected from %s", remoteAddr)
}

// multiplexPTYOutput reads from PTY master and writes to all sessions.
func (s *Server) multiplexPTYOutput() {
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-s.stopChan:
			return
		default:
		}

		// Set read deadline to allow checking stop channel
		s.ptyMaster.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, err := s.ptyMaster.Read(buf)
		if err != nil {
			if os.IsTimeout(err) {
				continue
			}
			if s.running {
				tui.DebugLogSSH("PTY read error: %v", err)
			}
			return
		}

		if n > 0 {
			data := buf[:n]
			s.sessionsMu.RLock()
			for session := range s.sessions {
				session.Write(data)
			}
			s.sessionsMu.RUnlock()
		}
	}
}

// DisconnectAllSessions closes all active SSH sessions.
// In a multiplexed PTY setup, all sessions share the same view, so when
// one user requests disconnect (Shift-Q), all sessions are disconnected.
func (s *Server) DisconnectAllSessions() {
	s.sessionsMu.Lock()
	sessions := make([]ssh.Session, 0, len(s.sessions))
	for session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessionsMu.Unlock()

	// Close sessions in goroutines to avoid blocking
	for _, session := range sessions {
		go func(sess ssh.Session) {
			tui.DebugLogSSH("Closing session from %s", sess.RemoteAddr().String())
			sess.Close()
		}(session)
	}

	if len(sessions) > 0 {
		tui.DebugLogSSH("Disconnecting %d session(s)", len(sessions))
	}
}
