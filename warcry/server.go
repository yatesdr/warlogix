// Package warcry provides a TCP server that streams PLC events to connected
// warcry notification clients. It broadcasts tag changes, health status, and
// tagpack updates as newline-delimited JSON, and responds to discovery queries.
package warcry

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// PLCProvider supplies current tag values and PLC names to the server.
type PLCProvider interface {
	GetAllCurrentValues() []TagSnapshot
	ListPLCNames() []string
}

// PackProvider supplies tagpack information to the server.
type PackProvider interface {
	ListPacks() []PackInfo
}

// TagSnapshot is the warcry-internal representation of a tag value.
type TagSnapshot struct {
	PLCName  string
	TagName  string
	Alias    string
	Address  string
	TypeName string
	Value    interface{}
	Writable bool
}

// PackInfo is the warcry-internal representation of a tagpack.
type PackInfo struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Members int    `json:"members"`
}

// Server is a TCP server that streams events to connected warcry clients.
type Server struct {
	mu         sync.RWMutex
	listener   net.Listener
	clients    map[uint64]*client
	nextID     uint64
	ringBuffer *RingBuffer
	running    bool
	stopChan   chan struct{}
	wg         sync.WaitGroup
	logFn      func(string, ...interface{})

	// Injected by engine for query responses.
	plcProv   PLCProvider
	packProv  PackProvider
	namespace string

	clientCount atomic.Int64
}

// client represents a single connected warcry client.
type client struct {
	id   uint64
	conn net.Conn
	send chan []byte
}

// NewServer creates a new warcry server (not yet listening).
func NewServer() *Server {
	return &Server{
		clients:  make(map[uint64]*client),
		stopChan: make(chan struct{}),
		logFn:    func(string, ...interface{}) {},
	}
}

// SetLogFunc sets the logging callback.
func (s *Server) SetLogFunc(fn func(string, ...interface{})) {
	s.logFn = fn
}

// SetPLCProvider sets the PLC data provider for snapshot/query responses.
func (s *Server) SetPLCProvider(p PLCProvider) {
	s.plcProv = p
}

// SetPackProvider sets the tagpack provider for query responses.
func (s *Server) SetPackProvider(p PackProvider) {
	s.packProv = p
}

// SetNamespace sets the namespace included in config responses.
func (s *Server) SetNamespace(ns string) {
	s.namespace = ns
}

// HasClients returns true if at least one client is connected.
// This is a fast atomic check so callers can skip serialization work.
func (s *Server) HasClients() bool {
	return s.clientCount.Load() > 0
}

// Start begins accepting TCP connections on the given address.
func (s *Server) Start(listenAddr string, bufferSize int) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("warcry listen: %w", err)
	}

	s.mu.Lock()
	s.listener = ln
	s.running = true
	s.ringBuffer = NewRingBuffer(bufferSize)
	s.mu.Unlock()

	s.logFn("Warcry connector listening on %s", listenAddr)

	s.wg.Add(1)
	go s.acceptLoop()
	return nil
}

// Stop shuts down the server and disconnects all clients.
func (s *Server) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopChan)
	s.listener.Close()

	for _, c := range s.clients {
		close(c.send)
		c.conn.Close()
	}
	s.clients = make(map[uint64]*client)
	s.clientCount.Store(0)
	s.mu.Unlock()

	s.wg.Wait()
	s.logFn("Warcry connector stopped")
}

// BroadcastTag sends a tag change event to all connected clients.
func (s *Server) BroadcastTag(plcName, tagName, alias, address, typeName string, value interface{}, writable bool) {
	msg := map[string]interface{}{
		"type":      "tag",
		"plc":       plcName,
		"tag":       tagName,
		"value":     value,
		"data_type": typeName,
		"writable":  writable,
		"ts":        time.Now().UTC().Format(time.RFC3339Nano),
	}
	if alias != "" {
		msg["alias"] = alias
	}
	if address != "" {
		msg["address"] = address
	}
	s.broadcast(msg)
}

// BroadcastHealth sends a PLC health event to all connected clients.
func (s *Server) BroadcastHealth(plcName, driver string, online bool, status, errMsg string) {
	msg := map[string]interface{}{
		"type":   "health",
		"plc":    plcName,
		"driver": driver,
		"online": online,
		"status": status,
		"ts":     time.Now().UTC().Format(time.RFC3339Nano),
	}
	if errMsg != "" {
		msg["error"] = errMsg
	}
	s.broadcast(msg)
}

// BroadcastTagPack sends a pre-serialized tagpack payload to all connected clients.
func (s *Server) BroadcastTagPack(name string, data []byte) {
	// Wrap the raw JSON data in an envelope.
	msg := map[string]interface{}{
		"type": "tagpack",
		"name": name,
		"data": json.RawMessage(data),
		"ts":   time.Now().UTC().Format(time.RFC3339Nano),
	}
	s.broadcast(msg)
}

// broadcast serializes a message, stores it in the ring buffer, and fans out
// to all connected clients (non-blocking).
func (s *Server) broadcast(msg map[string]interface{}) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')

	now := time.Now().UTC()

	s.mu.RLock()
	if s.ringBuffer != nil {
		s.ringBuffer.Add(data, now)
	}
	for _, c := range s.clients {
		select {
		case c.send <- data:
		default:
			// Slow client — drop event.
		}
	}
	s.mu.RUnlock()
}

// acceptLoop runs in its own goroutine and accepts new TCP connections.
func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stopChan:
				return
			default:
				s.logFn("Warcry accept error: %v", err)
				continue
			}
		}

		s.mu.Lock()
		id := s.nextID
		s.nextID++
		c := &client{
			id:   id,
			conn: conn,
			send: make(chan []byte, 256),
		}
		s.clients[id] = c
		s.clientCount.Add(1)
		s.mu.Unlock()

		s.logFn("Warcry client connected: %s (id=%d)", conn.RemoteAddr(), id)

		s.wg.Add(2)
		go s.clientWriter(c)
		go s.clientReader(c)

		// Send initial config + snapshot.
		go s.sendWelcome(c)
	}
}

// removeClient disconnects and removes a client.
func (s *Server) removeClient(c *client) {
	s.mu.Lock()
	if _, ok := s.clients[c.id]; ok {
		delete(s.clients, c.id)
		s.clientCount.Add(-1)
		close(c.send)
		c.conn.Close()
		s.logFn("Warcry client disconnected: %s (id=%d)", c.conn.RemoteAddr(), c.id)
	}
	s.mu.Unlock()
}

// clientWriter drains the send channel and writes to the TCP connection.
func (s *Server) clientWriter(c *client) {
	defer s.wg.Done()

	for data := range c.send {
		c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if _, err := c.conn.Write(data); err != nil {
			s.removeClient(c)
			return
		}
	}
}

// clientReader reads requests from the client and dispatches responses.
func (s *Server) clientReader(c *client) {
	defer s.wg.Done()
	defer s.removeClient(c)

	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req map[string]interface{}
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		msgType, _ := req["type"].(string)
		switch msgType {
		case "list_tags":
			s.handleListTags(c)
		case "list_packs":
			s.handleListPacks(c)
		case "get_config":
			s.sendConfig(c)
		case "replay":
			sinceStr, _ := req["since"].(string)
			s.handleReplay(c, sinceStr)
		}
	}
}

// sendWelcome sends the config response and a snapshot of current tag values.
func (s *Server) sendWelcome(c *client) {
	s.sendConfig(c)
	s.sendSnapshot(c)
}

// sendConfig sends the namespace config to a client.
func (s *Server) sendConfig(c *client) {
	msg := map[string]interface{}{
		"type":      "config",
		"namespace": s.namespace,
	}
	s.sendToClient(c, msg)
}

// sendSnapshot sends all current tag values as a single snapshot message.
func (s *Server) sendSnapshot(c *client) {
	if s.plcProv == nil {
		return
	}

	values := s.plcProv.GetAllCurrentValues()
	tags := make([]map[string]interface{}, 0, len(values))
	for _, v := range values {
		tag := map[string]interface{}{
			"plc":       v.PLCName,
			"tag":       v.TagName,
			"value":     v.Value,
			"data_type": v.TypeName,
			"writable":  v.Writable,
		}
		if v.Alias != "" {
			tag["alias"] = v.Alias
		}
		if v.Address != "" {
			tag["address"] = v.Address
		}
		tags = append(tags, tag)
	}

	msg := map[string]interface{}{
		"type": "snapshot",
		"tags": tags,
	}
	s.sendToClient(c, msg)
}

// handleListTags responds to a list_tags query.
func (s *Server) handleListTags(c *client) {
	if s.plcProv == nil {
		return
	}

	plcNames := s.plcProv.ListPLCNames()
	values := s.plcProv.GetAllCurrentValues()

	tags := make([]map[string]interface{}, 0, len(values))
	for _, v := range values {
		tag := map[string]interface{}{
			"plc":       v.PLCName,
			"tag":       v.TagName,
			"value":     v.Value,
			"data_type": v.TypeName,
			"writable":  v.Writable,
		}
		if v.Alias != "" {
			tag["alias"] = v.Alias
		}
		if v.Address != "" {
			tag["address"] = v.Address
		}
		tags = append(tags, tag)
	}

	msg := map[string]interface{}{
		"type": "tag_list",
		"plcs": plcNames,
		"tags": tags,
	}
	s.sendToClient(c, msg)
}

// handleListPacks responds to a list_packs query.
func (s *Server) handleListPacks(c *client) {
	if s.packProv == nil {
		return
	}

	packs := s.packProv.ListPacks()
	msg := map[string]interface{}{
		"type":  "pack_list",
		"packs": packs,
	}
	s.sendToClient(c, msg)
}

// handleReplay sends buffered events since the given timestamp.
// Recovers from panics caused by sending on a closed channel.
func (s *Server) handleReplay(c *client, sinceStr string) {
	defer func() { recover() }()

	ts, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		ts, err = time.Parse(time.RFC3339Nano, sinceStr)
		if err != nil {
			return
		}
	}

	s.mu.RLock()
	rb := s.ringBuffer
	s.mu.RUnlock()

	if rb == nil {
		return
	}

	entries := rb.Since(ts)
	for _, data := range entries {
		select {
		case c.send <- data:
		default:
			return // Client too slow, stop replay.
		}
	}
}

// sendToClient serializes and queues a message for a single client.
// Recovers from panics caused by sending on a closed channel, which can
// happen if the client disconnects while sendWelcome/handleReplay is running.
func (s *Server) sendToClient(c *client, msg map[string]interface{}) {
	defer func() { recover() }()

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	data = append(data, '\n')

	select {
	case c.send <- data:
	default:
		// Client too slow.
	}
}
