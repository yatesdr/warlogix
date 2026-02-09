package ssh

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/gdamore/tcell/v2/terminfo"
	gossh "golang.org/x/crypto/ssh"
	"warlink/api"
	"warlink/config"
	"warlink/kafka"
	"warlink/mqtt"
	"warlink/plcman"
	"warlink/tagpack"
	"warlink/trigger"
	"warlink/tui"
	"warlink/valkey"
)

// SharedManagers holds all the shared backend managers for daemon mode.
// Each SSH session creates its own TUI but shares these managers.
// Implements tui.SharedManagers interface.
type SharedManagers struct {
	Config     *config.Config
	ConfigPath string
	PLCMan     *plcman.Manager
	APIServer  *api.Server
	MQTTMgr    *mqtt.Manager
	ValkeyMgr  *valkey.Manager
	KafkaMgr   *kafka.Manager
	TriggerMgr *trigger.Manager
	PackMgr    *tagpack.Manager
}

// GetConfig returns the shared config.
func (m *SharedManagers) GetConfig() *config.Config { return m.Config }

// GetConfigPath returns the config file path.
func (m *SharedManagers) GetConfigPath() string { return m.ConfigPath }

// GetPLCMan returns the shared PLC manager.
func (m *SharedManagers) GetPLCMan() *plcman.Manager { return m.PLCMan }

// GetAPIServer returns the shared API server.
func (m *SharedManagers) GetAPIServer() *api.Server { return m.APIServer }

// GetMQTTMgr returns the shared MQTT manager.
func (m *SharedManagers) GetMQTTMgr() *mqtt.Manager { return m.MQTTMgr }

// GetValkeyMgr returns the shared Valkey manager.
func (m *SharedManagers) GetValkeyMgr() *valkey.Manager { return m.ValkeyMgr }

// GetKafkaMgr returns the shared Kafka manager.
func (m *SharedManagers) GetKafkaMgr() *kafka.Manager { return m.KafkaMgr }

// GetTriggerMgr returns the shared trigger manager.
func (m *SharedManagers) GetTriggerMgr() *trigger.Manager { return m.TriggerMgr }

// GetPackMgr returns the shared TagPack manager.
func (m *SharedManagers) GetPackMgr() *tagpack.Manager { return m.PackMgr }

// Session represents an active SSH session.
type Session struct {
	channel gossh.Channel
	conn    *gossh.ServerConn
	ptyReq  *ptyRequest
	tty     *SSHChannelTty
	closeMu sync.Mutex
	closed  bool
}

// RemoteAddr returns the remote address of the session.
func (s *Session) RemoteAddr() net.Addr {
	return s.conn.RemoteAddr()
}

// Close closes the session channel with proper SSH protocol signaling.
func (s *Session) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.tty != nil {
		s.tty.Stop()
	}
	// Send exit-status to signal shell has ended (SSH protocol)
	// Status 0 = success. This helps the client know the session ended cleanly.
	exitStatus := []byte{0, 0, 0, 0} // uint32 big-endian
	s.channel.SendRequest("exit-status", false, exitStatus)
	// Close write side to send EOF, then close the channel
	s.channel.CloseWrite()
	return s.channel.Close()
}

// CloseConnection closes the underlying SSH connection.
// This should be called after screen.Fini() to ensure terminal restore
// sequences are sent before the connection is terminated.
func (s *Session) CloseConnection() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// Window represents terminal window dimensions.
type Window struct {
	Width  int
	Height int
}

// ptyRequest holds PTY request parameters.
type ptyRequest struct {
	Term   string
	Width  uint32
	Height uint32
	// We ignore pixel dimensions and modes
}

// Server handles SSH connections with independent TUI per session.
type Server struct {
	config     *Config
	sshConfig  *gossh.ServerConfig
	listener   net.Listener
	managers   *SharedManagers
	sessions   map[*Session]struct{}
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
		sessions: make(map[*Session]struct{}),
		stopChan: make(chan struct{}),
	}
}

// SetSharedManagers sets the shared backend managers for the server.
func (s *Server) SetSharedManagers(m *SharedManagers) {
	s.managers = m
}

// SetOnSessionConnect sets a callback for when a session connects.
func (s *Server) SetOnSessionConnect(fn func(remoteAddr string)) {
	s.onSessionConnect = fn
}

// SetOnSessionDisconnect sets a callback for when a session disconnects.
func (s *Server) SetOnSessionDisconnect(fn func(remoteAddr string)) {
	s.onSessionDisconnect = fn
}

// Start starts the SSH server.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("server already running")
	}

	// Get or create host key
	hostKey, err := GetOrCreateHostKey()
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to get host key: %w", err)
	}

	// Build SSH server config
	s.sshConfig = &gossh.ServerConfig{}
	s.sshConfig.AddHostKey(hostKey)

	// Configure authentication
	hasAuth := false
	if s.config.Password != "" {
		s.sshConfig.PasswordCallback = passwordCallback(s.config.Password)
		hasAuth = true
	}
	if s.config.AuthorizedKeys != "" {
		callback := publicKeyCallback(s.config.AuthorizedKeys)
		if callback != nil {
			s.sshConfig.PublicKeyCallback = callback
			hasAuth = true
		}
	}

	// At least one auth method must be configured
	if !hasAuth {
		s.mu.Unlock()
		return fmt.Errorf("no authentication method configured")
	}

	// Start listener
	addr := fmt.Sprintf(":%d", s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}
	s.listener = listener
	s.running = true
	s.mu.Unlock()

	tui.DebugLogSSH("Server started on port %d", s.config.Port)

	// Accept connections in background
	go s.acceptLoop()

	return nil
}

// acceptLoop accepts incoming connections.
func (s *Server) acceptLoop() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopChan:
				return
			default:
				tui.DebugLogSSH("Accept error: %v", err)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

// handleConnection handles a single TCP connection.
func (s *Server) handleConnection(conn net.Conn) {
	// Perform SSH handshake
	sshConn, chans, reqs, err := gossh.NewServerConn(conn, s.sshConfig)
	if err != nil {
		tui.DebugLogSSH("SSH handshake failed from %s: %v", conn.RemoteAddr(), err)
		conn.Close()
		return
	}

	tui.DebugLogSSH("SSH connection from %s", sshConn.RemoteAddr())

	// Discard global requests
	go gossh.DiscardRequests(reqs)

	// Handle channels
	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(gossh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			tui.DebugLogSSH("Could not accept channel: %v", err)
			continue
		}

		go s.handleSession(sshConn, channel, requests)
	}
}

// handleSession handles an SSH session channel.
func (s *Server) handleSession(conn *gossh.ServerConn, channel gossh.Channel, requests <-chan *gossh.Request) {
	session := &Session{
		channel: channel,
		conn:    conn,
	}

	remoteAddr := conn.RemoteAddr().String()

	// Process session requests (pty-req, shell, window-change)
	ptyRequested := false

	for req := range requests {
		switch req.Type {
		case "pty-req":
			ptyReq, err := parsePtyRequest(req.Payload)
			if err != nil {
				tui.DebugLogSSH("Invalid pty-req from %s: %v", remoteAddr, err)
				if req.WantReply {
					req.Reply(false, nil)
				}
				continue
			}
			session.ptyReq = ptyReq
			ptyRequested = true
			if req.WantReply {
				req.Reply(true, nil)
			}

		case "shell":
			if req.WantReply {
				req.Reply(true, nil)
			}

			// Start session handling once we have both pty and shell
			if ptyRequested {
				go s.runSession(session)
			}

		case "window-change":
			win, err := parseWindowChange(req.Payload)
			if err != nil {
				tui.DebugLogSSH("Invalid window-change from %s: %v", remoteAddr, err)
				continue
			}
			if session.tty != nil {
				session.tty.SetWindowSize(win.Width, win.Height)
			}

		case "env":
			// Accept but ignore environment variables
			if req.WantReply {
				req.Reply(true, nil)
			}

		default:
			tui.DebugLogSSH("Unknown request type %s from %s", req.Type, remoteAddr)
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}

	// Channel closed, clean up
	session.Close()
}

// runSession runs the main session loop with an independent TUI.
func (s *Server) runSession(session *Session) {
	remoteAddr := session.RemoteAddr().String()
	tui.DebugLogSSH("Session started from %s (term=%s, size=%dx%d)",
		remoteAddr, session.ptyReq.Term, session.ptyReq.Width, session.ptyReq.Height)

	// Create SSHChannelTty from session's channel
	tty := NewSSHChannelTty(
		session.channel,
		session.ptyReq.Term,
		int(session.ptyReq.Width),
		int(session.ptyReq.Height),
	)
	session.tty = tty

	// Register session
	s.sessionsMu.Lock()
	s.sessions[session] = struct{}{}
	s.sessionsMu.Unlock()

	if s.onSessionConnect != nil {
		s.onSessionConnect(remoteAddr)
	}

	// Create tcell screen from the tty with proper terminfo
	screen, err := createScreenFromTty(tty)
	if err != nil {
		tui.DebugLogSSH("Failed to create screen for %s: %v", remoteAddr, err)
		s.cleanupSession(session, remoteAddr)
		return
	}

	// Create independent TUI instance with shared backend
	app, err := tui.NewAppWithSharedBackend(screen, s.managers)
	if err != nil {
		tui.DebugLogSSH("Failed to create TUI for %s: %v", remoteAddr, err)
		s.cleanupSession(session, remoteAddr)
		return
	}

	// Track if screen was finalized in disconnect callback
	screenFinalized := false

	// Set up disconnect callback - only closes THIS session
	app.SetOnDisconnect(func() {
		tui.DebugLogSSH("Disconnect requested from %s", remoteAddr)
		screenFinalized = true
		// Send terminal restore sequences BEFORE closing the channel.
		// These sequences exit alternate screen mode and show the cursor.
		// We send them directly because screen.Fini() will deadlock if called here,
		// and won't work after we close the channel.
		session.channel.Write([]byte("\x1b[?1049l\x1b[?25h\x1b[0m"))
		// Now close the channel to interrupt the blocked Read() in tcell's input goroutine
		tty.Close()
	})

	// Run TUI (blocks until session ends)
	// tview.Run() handles screen initialization internally
	if err := app.Run(); err != nil {
		tui.DebugLogSSH("TUI error for %s: %v", remoteAddr, err)
	}

	// Clean up
	app.Shutdown()
	// Only finalize screen if not already done in disconnect callback
	if !screenFinalized {
		screen.Fini()
	}
	session.CloseConnection()
	s.cleanupSession(session, remoteAddr)
}

// cleanupSession removes a session from the server and calls disconnect callback.
func (s *Server) cleanupSession(session *Session, remoteAddr string) {
	s.sessionsMu.Lock()
	delete(s.sessions, session)
	s.sessionsMu.Unlock()

	if s.onSessionDisconnect != nil {
		s.onSessionDisconnect(remoteAddr)
	}

	session.Close()
	tui.DebugLogSSH("Session disconnected from %s", remoteAddr)
}

// parsePtyRequest parses a pty-req payload.
func parsePtyRequest(payload []byte) (*ptyRequest, error) {
	// Format: string term, uint32 width, uint32 height, uint32 pixel_width, uint32 pixel_height, string modes
	if len(payload) < 4 {
		return nil, fmt.Errorf("payload too short")
	}

	termLen := binary.BigEndian.Uint32(payload[0:4])
	if len(payload) < int(4+termLen+16) {
		return nil, fmt.Errorf("payload too short for term")
	}

	term := string(payload[4 : 4+termLen])
	offset := 4 + termLen

	width := binary.BigEndian.Uint32(payload[offset : offset+4])
	height := binary.BigEndian.Uint32(payload[offset+4 : offset+8])
	// Skip pixel dimensions and modes

	return &ptyRequest{
		Term:   term,
		Width:  width,
		Height: height,
	}, nil
}

// parseWindowChange parses a window-change payload.
func parseWindowChange(payload []byte) (Window, error) {
	// Format: uint32 width, uint32 height, uint32 pixel_width, uint32 pixel_height
	if len(payload) < 8 {
		return Window{}, fmt.Errorf("payload too short")
	}

	width := binary.BigEndian.Uint32(payload[0:4])
	height := binary.BigEndian.Uint32(payload[4:8])

	return Window{
		Width:  int(width),
		Height: int(height),
	}, nil
}

// createScreenFromTty creates a tcell screen from an SSHChannelTty.
// It looks up the terminfo based on the terminal type from the SSH session.
func createScreenFromTty(tty *SSHChannelTty) (tcell.Screen, error) {
	term := tty.Term()

	// Try to look up terminfo for the terminal type
	ti, err := terminfo.LookupTerminfo(term)
	if err != nil {
		// Fall back to xterm-256color if the requested term isn't found
		tui.DebugLogSSH("Terminfo not found for %s, falling back to xterm-256color", term)
		ti, err = terminfo.LookupTerminfo("xterm-256color")
		if err != nil {
			// Last resort: try xterm
			ti, err = terminfo.LookupTerminfo("xterm")
			if err != nil {
				return nil, fmt.Errorf("failed to find terminfo: %w", err)
			}
		}
	}

	return tcell.NewTerminfoScreenFromTtyTerminfo(tty, ti)
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

// DisconnectAllSessions closes all active SSH sessions.
// Each session has its own TUI, so this method closes all of them.
func (s *Server) DisconnectAllSessions() {
	s.sessionsMu.Lock()
	sessions := make([]*Session, 0, len(s.sessions))
	for session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.sessionsMu.Unlock()

	// Close sessions in goroutines to avoid blocking
	for _, session := range sessions {
		go func(sess *Session) {
			tui.DebugLogSSH("Closing session from %s", sess.RemoteAddr().String())
			sess.Close()
		}(session)
	}

	if len(sessions) > 0 {
		tui.DebugLogSSH("Disconnecting %d session(s)", len(sessions))
	}
}
