package www

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"warlink/engine"
	"warlink/kafka"
	"warlink/logging"
	"warlink/plcman"
	"warlink/push"
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
	PLC            string `json:"plc"`
	Status         string `json:"status"` // "connected", "disconnected", "connecting", "error"
	StatusClass    string `json:"statusClass"`
	TagCount       int    `json:"tagCount"`
	Error          string `json:"error,omitempty"`
	ProductName    string `json:"productName,omitempty"`
	SerialNumber   string `json:"serialNumber,omitempty"`
	Vendor         string `json:"vendor,omitempty"`
	ConnectionMode string `json:"connectionMode,omitempty"`
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

// PushStatusUpdate represents a push webhook status change.
type PushStatusUpdate struct {
	Name         string `json:"name"`
	Status       string `json:"status"`
	StatusClass  string `json:"statusClass"`
	SendCount    int64  `json:"sendCount"`
	LastHTTPCode int    `json:"lastHTTPCode"`
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
					logging.DebugLog("browser", "SSE client %s buffer full, dropping %s event", client.id, event.Type)
				}
			}
			h.mu.RUnlock()

		case <-h.done:
			// Clean up all connected clients
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

// Stop stops the EventHub.
func (h *EventHub) Stop() {
	close(h.done)
}

// Broadcast sends an event to all connected clients.
func (h *EventHub) Broadcast(event SSEEvent) {
	select {
	case h.broadcast <- event:
	default:
		logging.DebugLog("browser", "SSE broadcast channel full, dropping %s event", event.Type)
	}
}

// broadcastEntityChange broadcasts an entity CRUD change to all clients.
func (h *EventHub) broadcastEntityChange(entityType, action, name string) {
	h.Broadcast(SSEEvent{
		Type: "entity-change",
		Data: EntityChangeUpdate{
			EntityType: entityType,
			Action:     action,
			Name:       name,
		},
	})
}

// broadcastConfigChange broadcasts a tag configuration change to all clients.
// This is a lightweight update that doesn't trigger a full tree refresh.
func (h *EventHub) broadcastConfigChange(plcName, tagName string, enabled, writable bool, ignores []string) {
	h.Broadcast(SSEEvent{
		Type: "config-change",
		Data: ConfigUpdate{
			PLC:      plcName,
			Tag:      tagName,
			Enabled:  enabled,
			Writable: writable,
			Ignores:  ignores,
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

// setupEventListeners registers listeners with plcman and engine EventBus to broadcast events.
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
					PLC:            plc.Config.Name,
					Status:         statusStr,
					StatusClass:    statusClass,
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

	// Subscribe to Engine EventBus for CRUD and config events.
	// This replaces manual broadcastEntityChange/broadcastConfigChange calls in handlers.
	if h.engine != nil {
	h.engine.Events.Subscribe(func(evt engine.Event) {
		switch evt.Type {
		// PLC CRUD
		case engine.EventPLCCreated:
			h.eventHub.broadcastEntityChange("plc", "add", evt.Payload.(engine.PLCEvent).Name)
		case engine.EventPLCUpdated:
			h.eventHub.broadcastEntityChange("plc", "update", evt.Payload.(engine.PLCEvent).Name)
		case engine.EventPLCDeleted:
			h.eventHub.broadcastEntityChange("plc", "remove", evt.Payload.(engine.PLCEvent).Name)

		// Tag config changes
		case engine.EventTagUpdated:
			te := evt.Payload.(engine.TagEvent)
			// Look up current tag config for the SSE payload
			plcCfg := cfg.FindPLC(te.PLCName)
			if plcCfg != nil {
				for _, tag := range plcCfg.Tags {
					if tag.Name == te.TagName {
						h.eventHub.broadcastConfigChange(te.PLCName, te.TagName, tag.Enabled, tag.Writable, tag.IgnoreChanges)
						break
					}
				}
			}
		case engine.EventTagCreated:
			te := evt.Payload.(engine.TagEvent)
			// Child tags (dotted paths) only need config-change (no tree refresh).
			// New root tags need entity-change to trigger tree refresh.
			if engine.IsChildTag(te.TagName) {
				plcCfg := cfg.FindPLC(te.PLCName)
				if plcCfg != nil {
					for _, tag := range plcCfg.Tags {
						if tag.Name == te.TagName {
							h.eventHub.broadcastConfigChange(te.PLCName, te.TagName, tag.Enabled, tag.Writable, tag.IgnoreChanges)
							break
						}
					}
				}
			} else {
				h.eventHub.broadcastEntityChange("plc", "update", te.PLCName)
			}
		case engine.EventTagDeleted:
			te := evt.Payload.(engine.TagEvent)
			h.eventHub.broadcastEntityChange("plc", "update", te.PLCName)

		// MQTT CRUD
		case engine.EventMQTTCreated:
			h.eventHub.broadcastEntityChange("mqtt", "add", evt.Payload.(engine.ServiceEvent).Name)
		case engine.EventMQTTUpdated:
			h.eventHub.broadcastEntityChange("mqtt", "update", evt.Payload.(engine.ServiceEvent).Name)
		case engine.EventMQTTDeleted:
			h.eventHub.broadcastEntityChange("mqtt", "remove", evt.Payload.(engine.ServiceEvent).Name)

		// Valkey CRUD
		case engine.EventValkeyCreated:
			h.eventHub.broadcastEntityChange("valkey", "add", evt.Payload.(engine.ServiceEvent).Name)
		case engine.EventValkeyUpdated:
			h.eventHub.broadcastEntityChange("valkey", "update", evt.Payload.(engine.ServiceEvent).Name)
		case engine.EventValkeyDeleted:
			h.eventHub.broadcastEntityChange("valkey", "remove", evt.Payload.(engine.ServiceEvent).Name)

		// Kafka CRUD
		case engine.EventKafkaCreated:
			h.eventHub.broadcastEntityChange("kafka", "add", evt.Payload.(engine.ServiceEvent).Name)
		case engine.EventKafkaUpdated:
			h.eventHub.broadcastEntityChange("kafka", "update", evt.Payload.(engine.ServiceEvent).Name)
		case engine.EventKafkaDeleted:
			h.eventHub.broadcastEntityChange("kafka", "remove", evt.Payload.(engine.ServiceEvent).Name)

		// TagPack CRUD
		case engine.EventTagPackCreated:
			h.eventHub.broadcastEntityChange("tagpack", "add", evt.Payload.(engine.TagPackEvent).Name)
		case engine.EventTagPackUpdated, engine.EventTagPackToggled:
			h.eventHub.broadcastEntityChange("tagpack", "update", evt.Payload.(engine.TagPackEvent).Name)
		case engine.EventTagPackDeleted:
			h.eventHub.broadcastEntityChange("tagpack", "remove", evt.Payload.(engine.TagPackEvent).Name)
		case engine.EventTagPackServiceToggled:
			h.eventHub.broadcastEntityChange("tagpack", "update", evt.Payload.(engine.TagPackServiceEvent).Name)
		case engine.EventTagPackMemberAdded, engine.EventTagPackMemberRemoved, engine.EventTagPackMemberIgnoreToggled:
			h.eventHub.broadcastEntityChange("tagpack", "update", evt.Payload.(engine.TagPackMemberEvent).PackName)

		// Trigger CRUD
		case engine.EventTriggerCreated:
			h.eventHub.broadcastEntityChange("trigger", "add", evt.Payload.(engine.TriggerEvent).Name)
		case engine.EventTriggerUpdated, engine.EventTriggerStarted, engine.EventTriggerStopped, engine.EventTriggerTestFired:
			h.eventHub.broadcastEntityChange("trigger", "update", evt.Payload.(engine.TriggerEvent).Name)
		case engine.EventTriggerDeleted:
			h.eventHub.broadcastEntityChange("trigger", "remove", evt.Payload.(engine.TriggerEvent).Name)
		case engine.EventTriggerTagAdded, engine.EventTriggerTagRemoved:
			h.eventHub.broadcastEntityChange("trigger", "update", evt.Payload.(engine.TriggerTagEvent).TriggerName)

		// Push CRUD
		case engine.EventPushCreated:
			h.eventHub.broadcastEntityChange("push", "add", evt.Payload.(engine.PushEvent).Name)
		case engine.EventPushUpdated:
			h.eventHub.broadcastEntityChange("push", "update", evt.Payload.(engine.PushEvent).Name)
		case engine.EventPushDeleted:
			h.eventHub.broadcastEntityChange("push", "remove", evt.Payload.(engine.PushEvent).Name)
		}
	})
	}

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
	lastPush := make(map[string]string)

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

			// Poll Push statuses
			if pushMgr := h.managers.GetPushMgr(); pushMgr != nil {
				for _, info := range pushMgr.GetAllPushInfo() {
					statusStr := pushStatusString(info.Status)
					if lastPush[info.Name] != statusStr {
						lastPush[info.Name] = statusStr
						h.eventHub.Broadcast(SSEEvent{
							Type: "push-status",
							Data: PushStatusUpdate{
								Name:         info.Name,
								Status:       statusStr,
								StatusClass:  "status-" + statusStr,
								SendCount:    info.SendCount,
								LastHTTPCode: info.LastHTTPCode,
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

// pushStatusString converts a push Status to a string.
func pushStatusString(s push.Status) string {
	switch s {
	case push.StatusArmed:
		return "armed"
	case push.StatusFiring:
		return "firing"
	case push.StatusWaitingClear:
		return "waiting"
	case push.StatusMinInterval:
		return "cooldown"
	case push.StatusError:
		return "error"
	default:
		return "disabled"
	}
}

