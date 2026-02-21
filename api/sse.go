package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yatesdr/plcio/logging"

	"warlink/plcman"
	"warlink/tagpack"
)

// SSE event type constants.
const (
	eventValueChange  = "value-change"
	eventTagpack      = "tagpack"
	eventStatusChange = "status-change"
	eventHealth       = "health"
)

// sseEvent is an internal event for the API SSE hub.
type sseEvent struct {
	Type string
	PLC  string // set when event is PLC-specific (for filtering)
	Tag  string // set when event is tag-specific (for filtering)
	Data interface{}
}

// apiValueUpdate is the JSON payload for value-change events.
type apiValueUpdate struct {
	PLC    string      `json:"plc"`
	Tag    string      `json:"tag"`
	MemLoc string      `json:"memloc,omitempty"`
	Value  interface{} `json:"value"`
	Type   string      `json:"type,omitempty"`
}

// apiStatusUpdate is the JSON payload for status-change events.
type apiStatusUpdate struct {
	PLC            string `json:"plc"`
	Status         string `json:"status"`
	TagCount       int    `json:"tagCount"`
	Error          string `json:"error,omitempty"`
	ProductName    string `json:"productName,omitempty"`
	SerialNumber   string `json:"serialNumber,omitempty"`
	Vendor         string `json:"vendor,omitempty"`
	ConnectionMode string `json:"connectionMode,omitempty"`
}

// apiHealthUpdate is the JSON payload for health events.
type apiHealthUpdate struct {
	PLC       string `json:"plc"`
	Driver    string `json:"driver"`
	Online    bool   `json:"online"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

// sseClient represents a connected SSE client.
type apiSSEClient struct {
	id     string
	events chan sseEvent
	done   chan struct{}
}

// eventHub manages SSE client connections and broadcasts events.
type eventHub struct {
	clients    map[string]*apiSSEClient
	register   chan *apiSSEClient
	unregister chan *apiSSEClient
	broadcast  chan sseEvent
	mu         sync.RWMutex
	done       chan struct{}
}

func newEventHub() *eventHub {
	hub := &eventHub{
		clients:    make(map[string]*apiSSEClient),
		register:   make(chan *apiSSEClient),
		unregister: make(chan *apiSSEClient),
		broadcast:  make(chan sseEvent, 256),
		done:       make(chan struct{}),
	}
	go hub.run()
	return hub
}

func (h *eventHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.id] = client
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.id]; ok {
				delete(h.clients, client.id)
				close(client.events)
			}
			h.mu.Unlock()

		case event := <-h.broadcast:
			h.mu.RLock()
			for _, client := range h.clients {
				select {
				case client.events <- event:
				default:
					logging.DebugLog("api-sse", "client %s buffer full, dropping %s event", client.id, event.Type)
				}
			}
			h.mu.RUnlock()

		case <-h.done:
			h.mu.Lock()
			for id, client := range h.clients {
				close(client.events)
				delete(h.clients, id)
			}
			h.mu.Unlock()
			return
		}
	}
}

func (h *eventHub) Broadcast(event sseEvent) {
	select {
	case h.broadcast <- event:
	default:
		logging.DebugLog("api-sse", "broadcast channel full, dropping %s event", event.Type)
	}
}

func (h *eventHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *eventHub) Stop() {
	close(h.done)
}

// handleSSE serves the /api/events SSE endpoint.
func (h *handlers) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// Parse filters from query params
	var typeFilter map[string]bool
	if types := r.URL.Query().Get("types"); types != "" {
		typeFilter = make(map[string]bool)
		for _, t := range strings.Split(types, ",") {
			typeFilter[strings.TrimSpace(t)] = true
		}
	}
	plcFilter := r.URL.Query().Get("plc")
	var plcsFilter map[string]bool
	if plcs := r.URL.Query().Get("plcs"); plcs != "" {
		plcsFilter = make(map[string]bool)
		for _, p := range strings.Split(plcs, ",") {
			plcsFilter[strings.TrimSpace(p)] = true
		}
	}
	var tagFilter map[string]bool
	if tags := r.URL.Query().Get("tags"); tags != "" {
		tagFilter = make(map[string]bool)
		for _, t := range strings.Split(tags, ",") {
			tagFilter[strings.TrimSpace(t)] = true
		}
	}

	clientID := fmt.Sprintf("api-%d", time.Now().UnixNano())
	client := &apiSSEClient{
		id:     clientID,
		events: make(chan sseEvent, 64),
		done:   make(chan struct{}),
	}

	h.hub.register <- client

	notify := r.Context().Done()

	fmt.Fprintf(w, "event: connected\ndata: {\"id\":%q}\n\n", clientID)
	flusher.Flush()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-notify:
			h.hub.unregister <- client
			return

		case event, ok := <-client.events:
			if !ok {
				return
			}
			// Apply type filter
			if typeFilter != nil && !typeFilter[event.Type] {
				continue
			}
			// Apply PLC filter (only to PLC-specific events)
			if plcFilter != "" && event.PLC != "" && event.PLC != plcFilter {
				continue
			}
			if plcsFilter != nil && event.PLC != "" && !plcsFilter[event.PLC] {
				continue
			}
			// Apply tag filter (only to tag-specific events)
			if tagFilter != nil && event.Tag != "" && !tagFilter[event.Tag] {
				continue
			}
			data, err := json.Marshal(event.Data)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(data))
			flusher.Flush()

		case <-ticker.C:
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// setupSSE wires up PLC and tagpack listeners to broadcast SSE events.
// Returns a cleanup function that removes all listeners and stops the hub.
func (h *handlers) setupSSE() func() {
	plcMan := h.managers.GetPLCMan()

	// Value change listener
	h.valueListenerID = plcMan.AddOnValueChangeListener(func(changes []plcman.ValueChange) {
		for _, change := range changes {
			if change.NoREST {
				continue
			}
			tag := change.TagName
			memloc := ""
			if change.Alias != "" {
				tag = change.Alias
				memloc = change.TagName
			}
			h.hub.Broadcast(sseEvent{
				Type: eventValueChange,
				PLC:  change.PLCName,
				Tag:  tag,
				Data: apiValueUpdate{
					PLC:    change.PLCName,
					Tag:    tag,
					MemLoc: memloc,
					Value:  change.Value,
					Type:   change.TypeName,
				},
			})
		}
	})

	// PLC status change listener
	h.changeListenerID = plcMan.AddOnChangeListener(func() {
		plcs := plcMan.ListPLCs()
		for _, plc := range plcs {
			status := plc.GetStatus()
			statusStr := "disconnected"
			switch status {
			case plcman.StatusConnected:
				statusStr = "connected"
			case plcman.StatusConnecting:
				statusStr = "connecting"
			case plcman.StatusError:
				statusStr = "error"
			}

			errMsg := ""
			if plc.GetError() != nil {
				errMsg = plc.GetError().Error()
			}

			var productName, serialNumber, vendor string
			if info := plc.GetDeviceInfo(); info != nil {
				productName = info.Model
				serialNumber = info.SerialNumber
				vendor = info.Vendor
			}

			h.hub.Broadcast(sseEvent{
				Type: eventStatusChange,
				PLC:  plc.Config.Name,
				Data: apiStatusUpdate{
					PLC:            plc.Config.Name,
					Status:         statusStr,
					TagCount:       len(plc.GetTags()),
					Error:          errMsg,
					ProductName:    productName,
					SerialNumber:   serialNumber,
					Vendor:         vendor,
					ConnectionMode: plc.GetConnectionMode(),
				},
			})
		}
	})

	// TagPack publish listener
	packMgr := h.managers.GetPackMgr()
	if packMgr != nil {
		h.packListenerID = packMgr.AddOnPublishListener(func(info tagpack.PackPublishInfo) {
			h.hub.Broadcast(sseEvent{
				Type: eventTagpack,
				Data: info.Value,
			})
		})
	}

	// Health polling goroutine
	go h.pollHealth()

	return func() {
		h.hub.Stop()
		plcMan.RemoveOnValueChangeListener(h.valueListenerID)
		plcMan.RemoveOnChangeListener(h.changeListenerID)
		if packMgr != nil {
			packMgr.RemoveOnPublishListener(h.packListenerID)
		}
	}
}

// pollHealth broadcasts health events for all PLCs on a 10s ticker.
func (h *handlers) pollHealth() {
	// Initial delay to let PLCs connect
	select {
	case <-time.After(2 * time.Second):
	case <-h.hub.done:
		return
	}

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-h.hub.done:
			return
		case <-ticker.C:
			if h.hub.ClientCount() == 0 {
				continue
			}
			plcMan := h.managers.GetPLCMan()
			for _, plc := range plcMan.ListPLCs() {
				if !plc.Config.IsHealthCheckEnabled() {
					continue
				}
				health := plc.GetHealthStatus()
				h.hub.Broadcast(sseEvent{
					Type: eventHealth,
					PLC:  plc.Config.Name,
					Data: apiHealthUpdate{
						PLC:       plc.Config.Name,
						Driver:    health.Driver,
						Online:    health.Online,
						Status:    health.Status,
						Error:     health.Error,
						Timestamp: health.Timestamp.Format(time.RFC3339),
					},
				})
			}
		}
	}
}
