package www

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"warlink/kafka"
	"warlink/plcman"
	"warlink/trigger"
	"warlink/tui"
)

// SSEEvent represents an event to broadcast to SSE clients.
type SSEEvent struct {
	Type string      `json:"type"` // "value-change", "config-change", "status-change", "mqtt-status", "valkey-status", "kafka-status", "trigger-status", "debug-log", "entity-change"
	Data interface{} `json:"data"`
}

// ValueUpdate represents a tag value change event.
type ValueUpdate struct {
	PLC         string      `json:"plc"`
	Tag         string      `json:"tag"`
	Value       interface{} `json:"value"`
	Type        string      `json:"type,omitempty"`
	LastChanged string      `json:"lastChanged,omitempty"`
}

// ConfigUpdate represents a tag configuration change event.
type ConfigUpdate struct {
	PLC      string   `json:"plc"`
	Tag      string   `json:"tag"`
	Enabled  bool     `json:"enabled"`
	Writable bool     `json:"writable"`
	Ignores  []string `json:"ignores"`
}

// StatusUpdate represents a PLC connection status change event.
type StatusUpdate struct {
	PLC          string `json:"plc"`
	Status       string `json:"status"` // "connected", "disconnected", "connecting", "error"
	StatusClass  string `json:"statusClass"`
	TagCount     int    `json:"tagCount"`
	Error        string `json:"error,omitempty"`
	ProductName  string `json:"productName,omitempty"`
	SerialNumber string `json:"serialNumber,omitempty"`
	Vendor       string `json:"vendor,omitempty"`
}

// MQTTStatusUpdate represents an MQTT broker status change.
type MQTTStatusUpdate struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // "connected", "disconnected"
	StatusClass string `json:"statusClass"`
}

// ValkeyStatusUpdate represents a Valkey publisher status change.
type ValkeyStatusUpdate struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // "connected", "disconnected"
	StatusClass string `json:"statusClass"`
}

// KafkaStatusUpdate represents a Kafka producer status change.
type KafkaStatusUpdate struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // "connected", "disconnected", "connecting", "error"
	StatusClass string `json:"statusClass"`
	MessagesSent int64  `json:"messagesSent"`
	Errors       int64  `json:"errors"`
}

// TriggerStatusUpdate represents a trigger status change.
type TriggerStatusUpdate struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // "disabled", "armed", "firing", "cooldown", "error"
	StatusClass string `json:"statusClass"`
	FireCount   int64  `json:"fireCount"`
	LastFire    string `json:"lastFire,omitempty"`
	Error       string `json:"error,omitempty"`
}

// DebugLogUpdate represents a debug log entry.
type DebugLogUpdate struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

// EntityChangeUpdate represents an entity CRUD change.
type EntityChangeUpdate struct {
	EntityType string `json:"entityType"` // "mqtt", "valkey", "kafka", "trigger", "tagpack", "plc"
	Action     string `json:"action"`     // "add", "update", "remove"
	Name       string `json:"name"`
}

// sseClient represents a connected SSE client.
type sseClient struct {
	id     string
	events chan SSEEvent
	done   chan struct{}
}

// EventHub manages SSE client connections and broadcasts events.
type EventHub struct {
	clients    map[string]*sseClient
	register   chan *sseClient
	unregister chan *sseClient
	broadcast  chan SSEEvent
	mu         sync.RWMutex
	done       chan struct{}
}

// newEventHub creates a new EventHub.
func newEventHub() *EventHub {
	hub := &EventHub{
		clients:    make(map[string]*sseClient),
		register:   make(chan *sseClient),
		unregister: make(chan *sseClient),
		broadcast:  make(chan SSEEvent, 256),
		done:       make(chan struct{}),
	}
	go hub.run()
	return hub
}

// run processes client registration and event broadcasting.
func (h *EventHub) run() {
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
					log.Printf("[SSE] client %s buffer full, dropping %s event", client.id, event.Type)
				}
			}
			h.mu.RUnlock()

		case <-h.done:
			return
		}
	}
}

// Stop stops the EventHub.
func (h *EventHub) Stop() {
	close(h.done)
}

// Broadcast sends an event to all connected clients.
func (h *EventHub) Broadcast(event SSEEvent) {
	select {
	case h.broadcast <- event:
	default:
		log.Printf("[SSE] broadcast channel full, dropping %s event", event.Type)
	}
}

// BroadcastEntityChange broadcasts an entity CRUD change to all clients.
func (h *EventHub) BroadcastEntityChange(entityType, action, name string) {
	h.Broadcast(SSEEvent{
		Type: "entity-change",
		Data: EntityChangeUpdate{
			EntityType: entityType,
			Action:     action,
			Name:       name,
		},
	})
}

// ClientCount returns the number of connected clients.
func (h *EventHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// handleSSERepublisher handles SSE connections for the republisher page.
func (h *Handlers) handleSSERepublisher(w http.ResponseWriter, r *http.Request) {
	// Check if SSE is supported
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create client
	clientID := fmt.Sprintf("client-%d", time.Now().UnixNano())
	client := &sseClient{
		id:     clientID,
		events: make(chan SSEEvent, 64),
		done:   make(chan struct{}),
	}

	// Register client
	h.eventHub.register <- client

	// Set up cleanup on disconnect
	notify := r.Context().Done()

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"id\":\"%s\"}\n\n", clientID)
	flusher.Flush()

	// Keepalive ticker
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Event loop
	for {
		select {
		case <-notify:
			// Client disconnected
			h.eventHub.unregister <- client
			return

		case event, ok := <-client.events:
			if !ok {
				// Channel closed
				return
			}
			// Serialize event data
			data, err := json.Marshal(event.Data)
			if err != nil {
				continue
			}
			// Send event
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, string(data))
			flusher.Flush()

		case <-ticker.C:
			// Send keepalive comment
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		}
	}
}

// tagConfigSnapshot stores the last known tag config state for diffing.
type tagConfigSnapshot struct {
	Enabled  bool
	Writable bool
	Ignores  string // JSON-encoded ignores for comparison
}

// setupEventListeners registers listeners with plcman and config to broadcast events.
func (h *Handlers) setupEventListeners() {
	plcMan := h.managers.GetPLCMan()
	cfg := h.managers.GetConfig()

	// Listen for value changes
	plcMan.AddOnValueChangeListener(func(changes []plcman.ValueChange) {
		for _, change := range changes {
			h.eventHub.Broadcast(SSEEvent{
				Type: "value-change",
				Data: ValueUpdate{
					PLC:   change.PLCName,
					Tag:   change.TagName,
					Value: change.Value,
					Type:  change.TypeName,
				},
			})
		}
	})

	// Listen for PLC status changes
	plcMan.AddOnChangeListener(func() {
		// Get current status of all PLCs and broadcast
		plcs := plcMan.ListPLCs()
		for _, plc := range plcs {
			status := plc.GetStatus()
			statusStr := "disconnected"
			statusClass := "status-disconnected"
			switch status {
			case plcman.StatusConnected:
				statusStr = "connected"
				statusClass = "status-connected"
			case plcman.StatusConnecting:
				statusStr = "connecting"
				statusClass = "status-connecting"
			case plcman.StatusError:
				statusStr = "error"
				statusClass = "status-error"
			}

			// Get additional info
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

			h.eventHub.Broadcast(SSEEvent{
				Type: "status-change",
				Data: StatusUpdate{
					PLC:          plc.Config.Name,
					Status:       statusStr,
					StatusClass:  statusClass,
					TagCount:     len(plc.GetTags()),
					Error:        errMsg,
					ProductName:  productName,
					SerialNumber: serialNumber,
					Vendor:       vendor,
				},
			})
		}
	})

	// Listen for config changes with diff-only broadcasting
	var lastTagConfigs sync.Map // map[string]tagConfigSnapshot, key = "plc:tag"

	// Pre-populate snapshot with current config to avoid flooding on first change
	for _, plcCfg := range cfg.PLCs {
		for _, tag := range plcCfg.Tags {
			key := plcCfg.Name + ":" + tag.Name
			ignoresJSON, _ := json.Marshal(tag.IgnoreChanges)
			lastTagConfigs.Store(key, tagConfigSnapshot{
				Enabled:  tag.Enabled,
				Writable: tag.Writable,
				Ignores:  string(ignoresJSON),
			})
		}
	}

	cfg.AddOnChangeListener(func() {
		for _, plcCfg := range cfg.PLCs {
			for _, tag := range plcCfg.Tags {
				key := plcCfg.Name + ":" + tag.Name
				ignoresJSON, _ := json.Marshal(tag.IgnoreChanges)
				current := tagConfigSnapshot{
					Enabled:  tag.Enabled,
					Writable: tag.Writable,
					Ignores:  string(ignoresJSON),
				}

				if prev, ok := lastTagConfigs.Load(key); ok {
					prevSnap := prev.(tagConfigSnapshot)
					if prevSnap == current {
						continue // No change, skip broadcast
					}
				}
				lastTagConfigs.Store(key, current)

				h.eventHub.Broadcast(SSEEvent{
					Type: "config-change",
					Data: ConfigUpdate{
						PLC:      plcCfg.Name,
						Tag:      tag.Name,
						Enabled:  tag.Enabled,
						Writable: tag.Writable,
						Ignores:  tag.IgnoreChanges,
					},
				})
			}
		}
	})

	// Subscribe to debug log messages
	if store := tui.GetDebugStore(); store != nil {
		store.Subscribe(func(msg tui.LogMessage) {
			h.eventHub.Broadcast(SSEEvent{
				Type: "debug-log",
				Data: DebugLogUpdate{
					Timestamp: msg.Timestamp.Format("2006-01-02 15:04:05"),
					Level:     msg.Level,
					Message:   msg.Message,
				},
			})
		})
	}

	// Start service status polling goroutine
	go h.pollServiceStatuses()
}

// pollServiceStatuses periodically checks MQTT/Valkey/Kafka/Trigger statuses and broadcasts changes.
func (h *Handlers) pollServiceStatuses() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Track last-known statuses
	lastMQTT := make(map[string]string)
	lastValkey := make(map[string]string)
	lastKafka := make(map[string]string)
	lastTrigger := make(map[string]string)

	for {
		select {
		case <-h.eventHub.done:
			return
		case <-ticker.C:
			// Only poll if there are connected clients
			if h.eventHub.ClientCount() == 0 {
				continue
			}

			// Poll MQTT statuses
			if mqttMgr := h.managers.GetMQTTMgr(); mqttMgr != nil {
				for _, pub := range mqttMgr.List() {
					name := pub.Name()
					status := "disconnected"
					if pub.IsRunning() {
						status = "connected"
					}
					if lastMQTT[name] != status {
						lastMQTT[name] = status
						statusClass := "status-disconnected"
						if status == "connected" {
							statusClass = "status-connected"
						}
						h.eventHub.Broadcast(SSEEvent{
							Type: "mqtt-status",
							Data: MQTTStatusUpdate{
								Name:        name,
								Status:      status,
								StatusClass: statusClass,
							},
						})
					}
				}
			}

			// Poll Valkey statuses
			if valkeyMgr := h.managers.GetValkeyMgr(); valkeyMgr != nil {
				for _, pub := range valkeyMgr.List() {
					name := pub.Config().Name
					status := "disconnected"
					if pub.IsRunning() {
						status = "connected"
					}
					if lastValkey[name] != status {
						lastValkey[name] = status
						statusClass := "status-disconnected"
						if status == "connected" {
							statusClass = "status-connected"
						}
						h.eventHub.Broadcast(SSEEvent{
							Type: "valkey-status",
							Data: ValkeyStatusUpdate{
								Name:        name,
								Status:      status,
								StatusClass: statusClass,
							},
						})
					}
				}
			}

			// Poll Kafka statuses
			if kafkaMgr := h.managers.GetKafkaMgr(); kafkaMgr != nil {
				for _, name := range kafkaMgr.ListClusters() {
					producer := kafkaMgr.GetProducer(name)
					if producer == nil {
						continue
					}
					prodStatus := producer.GetStatus()
					statusStr := kafkaStatusString(prodStatus)
					sent, errs, _ := producer.GetStats()

					if lastKafka[name] != statusStr {
						lastKafka[name] = statusStr
						h.eventHub.Broadcast(SSEEvent{
							Type: "kafka-status",
							Data: KafkaStatusUpdate{
								Name:         name,
								Status:       statusStr,
								StatusClass:  "status-" + statusStr,
								MessagesSent: sent,
								Errors:       errs,
							},
						})
					}
				}
			}

			// Poll Trigger statuses
			if triggerMgr := h.managers.GetTriggerMgr(); triggerMgr != nil {
				for _, info := range triggerMgr.GetAllTriggerInfo() {
					statusStr := triggerStatusString(info.Status)
					if lastTrigger[info.Name] != statusStr {
						lastTrigger[info.Name] = statusStr
						errMsg := ""
						if info.Error != nil {
							errMsg = info.Error.Error()
						}
						lastFireStr := ""
						if !info.LastFire.IsZero() {
							lastFireStr = info.LastFire.Format("2006-01-02 15:04:05")
						}
						h.eventHub.Broadcast(SSEEvent{
							Type: "trigger-status",
							Data: TriggerStatusUpdate{
								Name:        info.Name,
								Status:      statusStr,
								StatusClass: "status-" + statusStr,
								FireCount:   info.FireCount,
								LastFire:    lastFireStr,
								Error:       errMsg,
							},
						})
					}
				}
			}
		}
	}
}

// kafkaStatusString converts a Kafka ConnectionStatus to a string.
func kafkaStatusString(s kafka.ConnectionStatus) string {
	switch s {
	case kafka.StatusConnected:
		return "connected"
	case kafka.StatusConnecting:
		return "connecting"
	case kafka.StatusError:
		return "error"
	default:
		return "disconnected"
	}
}

// triggerStatusString converts a trigger Status to a string.
func triggerStatusString(s trigger.Status) string {
	switch s {
	case trigger.StatusArmed:
		return "armed"
	case trigger.StatusFiring:
		return "firing"
	case trigger.StatusCooldown:
		return "cooldown"
	case trigger.StatusError:
		return "error"
	default:
		return "disabled"
	}
}

// getRepublisherTagData returns tag data for SSE updates.
func (h *Handlers) getRepublisherTagData(plcName, tagName string) *RepublisherTag {
	manager := h.managers.GetPLCMan()
	plc := manager.GetPLC(plcName)
	if plc == nil {
		return nil
	}

	cfg := h.managers.GetConfig()
	plcCfg := cfg.FindPLC(plcName)
	if plcCfg == nil {
		return nil
	}

	// Build config lookup for this tag and child tags map
	configMap := make(map[string]*struct {
		Alias         string
		Enabled       bool
		Writable      bool
		IgnoreChanges []string
	})
	childTagsMap := make(map[string]map[string]PublishedChild)

	for i := range plcCfg.Tags {
		sel := &plcCfg.Tags[i]
		configMap[sel.Name] = &struct {
			Alias         string
			Enabled       bool
			Writable      bool
			IgnoreChanges []string
		}{
			Alias:         sel.Alias,
			Enabled:       sel.Enabled,
			Writable:      sel.Writable,
			IgnoreChanges: sel.IgnoreChanges,
		}

		// Check if this is a child tag
		if idx := strings.Index(sel.Name, "."); idx > 0 {
			parentName := sel.Name[:idx]
			childPath := sel.Name[idx+1:]
			if childTagsMap[parentName] == nil {
				childTagsMap[parentName] = make(map[string]PublishedChild)
			}
			childTagsMap[parentName][childPath] = PublishedChild{
				Enabled:  sel.Enabled,
				Writable: sel.Writable,
			}
		}
	}

	// Get tag info
	tags := plc.GetTags()
	values := plc.GetValues()
	var lastChanged map[string]time.Time // not tracked yet

	for _, tag := range tags {
		if tag.Name == tagName {
			lastPollStr := ""
			if !plc.LastPoll.IsZero() {
				lastPollStr = plc.LastPoll.Format("2006-01-02 15:04:05")
			}
			rt := h.buildRepublisherTag(tag.Name, tag.TypeName, configMap, childTagsMap, values, lastChanged, lastPollStr)
			return &rt
		}
	}

	return nil
}
