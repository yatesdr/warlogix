package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"warlink/config"
	"warlink/engine"
)

// writeEngineError maps engine sentinel errors to HTTP status codes.
func (h *handlers) writeEngineError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, engine.ErrNotFound):
		h.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, engine.ErrAlreadyExists):
		h.writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, engine.ErrInvalidInput):
		h.writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, engine.ErrSaveFailed):
		h.writeError(w, http.StatusInternalServerError, err.Error())
	default:
		h.writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (h *handlers) requireEngine(w http.ResponseWriter) bool {
	if h.engine == nil {
		h.writeError(w, http.StatusServiceUnavailable, "mutation API not available")
		return false
	}
	return true
}

// --- PLC ---

type plcRequest struct {
	Name               string `json:"name"`
	Address            string `json:"address"`
	Slot               int    `json:"slot"`
	Family             string `json:"family"`
	Enabled            bool   `json:"enabled"`
	HealthCheckEnabled *bool  `json:"health_check_enabled"`
	DiscoverTags       *bool  `json:"discover_tags"`
	PollRate           string `json:"poll_rate"`
	Timeout            string `json:"timeout"`
	AmsNetId           string `json:"ams_net_id"`
	AmsPort            int    `json:"ams_port"`
	Protocol           string `json:"protocol"`
	FinsPort           int    `json:"fins_port"`
	FinsNetwork        int    `json:"fins_network"`
	FinsNode           int    `json:"fins_node"`
	FinsUnit           int    `json:"fins_unit"`
}

func (h *handlers) handleCreatePLC(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req plcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var pollRate, timeout time.Duration
	if req.PollRate != "" {
		if d, err := time.ParseDuration(req.PollRate); err == nil {
			pollRate = d
		}
	}
	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = d
		}
	}
	engReq := engine.PLCCreateRequest{
		Name: req.Name, Address: req.Address, Slot: byte(req.Slot),
		Enabled: req.Enabled, HealthCheckEnabled: req.HealthCheckEnabled,
		DiscoverTags: req.DiscoverTags, PollRate: pollRate, Timeout: timeout,
		AmsNetId: req.AmsNetId, AmsPort: uint16(req.AmsPort),
		Protocol: req.Protocol, FinsPort: req.FinsPort,
		FinsNetwork: byte(req.FinsNetwork), FinsNode: byte(req.FinsNode), FinsUnit: byte(req.FinsUnit),
	}
	if req.Family != "" {
		engReq.Family = config.PLCFamily(req.Family)
	}
	if err := h.engine.CreatePLC(engReq); err != nil {
		h.writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"status": "created"})
}

func (h *handlers) handleUpdatePLC(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	var req plcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var pollRate, timeout time.Duration
	if req.PollRate != "" {
		if d, err := time.ParseDuration(req.PollRate); err == nil {
			pollRate = d
		}
	}
	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = d
		}
	}
	engReq := engine.PLCUpdateRequest{
		Address: req.Address, Slot: byte(req.Slot),
		Enabled: req.Enabled, HealthCheckEnabled: req.HealthCheckEnabled,
		DiscoverTags: req.DiscoverTags, PollRate: pollRate, Timeout: timeout,
		AmsNetId: req.AmsNetId, AmsPort: uint16(req.AmsPort),
		Protocol: req.Protocol, FinsPort: req.FinsPort,
		FinsNetwork: byte(req.FinsNetwork), FinsNode: byte(req.FinsNode), FinsUnit: byte(req.FinsUnit),
	}
	if req.Family != "" {
		engReq.Family = config.PLCFamily(req.Family)
	}
	if err := h.engine.UpdatePLC(name, engReq); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "updated"})
}

func (h *handlers) handleDeletePLC(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.DeletePLC(name); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "deleted"})
}

func (h *handlers) handleConnectPLC(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.ConnectPLC(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "connecting"})
}

func (h *handlers) handleDisconnectPLC(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	h.engine.DisconnectPLC(name)
	h.writeJSON(w, map[string]string{"status": "disconnected"})
}

// --- MQTT ---

type mqttAPIRequest struct {
	Name     string `json:"name"`
	Broker   string `json:"broker"`
	Port     int    `json:"port"`
	ClientID string `json:"client_id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Selector string `json:"selector"`
	UseTLS   bool   `json:"use_tls"`
	Enabled  bool   `json:"enabled"`
}

func (h *handlers) handleCreateMQTT(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req mqttAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.CreateMQTT(engine.MQTTCreateRequest{
		Name: req.Name, Broker: req.Broker, Port: req.Port,
		ClientID: req.ClientID, Username: req.Username, Password: req.Password,
		Selector: req.Selector, UseTLS: req.UseTLS, Enabled: req.Enabled,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"status": "created"})
}

func (h *handlers) handleUpdateMQTT(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	var req mqttAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.UpdateMQTT(name, engine.MQTTUpdateRequest{
		Broker: req.Broker, Port: req.Port,
		ClientID: req.ClientID, Username: req.Username, Password: req.Password,
		Selector: req.Selector, UseTLS: req.UseTLS, Enabled: req.Enabled,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "updated"})
}

func (h *handlers) handleDeleteMQTT(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.DeleteMQTT(name); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "deleted"})
}

func (h *handlers) handleStartMQTT(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.StartMQTT(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "started"})
}

func (h *handlers) handleStopMQTT(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	h.engine.StopMQTT(name)
	h.writeJSON(w, map[string]string{"status": "stopped"})
}

// --- Valkey ---

type valkeyAPIRequest struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	Password        string `json:"password"`
	Database        int    `json:"database"`
	Selector        string `json:"selector"`
	KeyTTL          string `json:"key_ttl"`
	UseTLS          bool   `json:"use_tls"`
	PublishChanges  bool   `json:"publish_changes"`
	EnableWriteback bool   `json:"enable_writeback"`
	Enabled         bool   `json:"enabled"`
}

func (h *handlers) handleCreateValkey(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req valkeyAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var keyTTL time.Duration
	if req.KeyTTL != "" {
		if d, err := time.ParseDuration(req.KeyTTL); err == nil {
			keyTTL = d
		}
	}
	if err := h.engine.CreateValkey(engine.ValkeyCreateRequest{
		Name: req.Name, Address: req.Address, Password: req.Password,
		Database: req.Database, Selector: req.Selector, KeyTTL: keyTTL,
		UseTLS: req.UseTLS, PublishChanges: req.PublishChanges,
		EnableWriteback: req.EnableWriteback, Enabled: req.Enabled,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"status": "created"})
}

func (h *handlers) handleUpdateValkey(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	var req valkeyAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var keyTTL time.Duration
	if req.KeyTTL != "" {
		if d, err := time.ParseDuration(req.KeyTTL); err == nil {
			keyTTL = d
		}
	}
	if err := h.engine.UpdateValkey(name, engine.ValkeyUpdateRequest{
		Address: req.Address, Password: req.Password,
		Database: req.Database, Selector: req.Selector, KeyTTL: keyTTL,
		UseTLS: req.UseTLS, PublishChanges: req.PublishChanges,
		EnableWriteback: req.EnableWriteback, Enabled: req.Enabled,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "updated"})
}

func (h *handlers) handleDeleteValkey(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.DeleteValkey(name); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "deleted"})
}

func (h *handlers) handleStartValkey(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.StartValkey(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "started"})
}

func (h *handlers) handleStopValkey(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	h.engine.StopValkey(name)
	h.writeJSON(w, map[string]string{"status": "stopped"})
}

// --- Kafka ---

type kafkaAPIRequest struct {
	Name             string   `json:"name"`
	Brokers          []string `json:"brokers"`
	UseTLS           bool     `json:"use_tls"`
	TLSSkipVerify    bool     `json:"tls_skip_verify"`
	SASLMechanism    string   `json:"sasl_mechanism"`
	Username         string   `json:"username"`
	Password         string   `json:"password"`
	Selector         string   `json:"selector"`
	PublishChanges   bool     `json:"publish_changes"`
	EnableWriteback  bool     `json:"enable_writeback"`
	AutoCreateTopics bool     `json:"auto_create_topics"`
	Enabled          bool     `json:"enabled"`
	RequiredAcks     int      `json:"required_acks"`
	MaxRetries       int      `json:"max_retries"`
	RetryBackoff     string   `json:"retry_backoff"`
	ConsumerGroup    string   `json:"consumer_group"`
	WriteMaxAge      string   `json:"write_max_age"`
}

func (h *handlers) handleCreateKafka(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req kafkaAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var retryBackoff, writeMaxAge time.Duration
	if req.RetryBackoff != "" {
		if d, err := time.ParseDuration(req.RetryBackoff); err == nil {
			retryBackoff = d
		}
	}
	if req.WriteMaxAge != "" {
		if d, err := time.ParseDuration(req.WriteMaxAge); err == nil {
			writeMaxAge = d
		}
	}
	if err := h.engine.CreateKafka(engine.KafkaCreateRequest{
		Name: req.Name, Brokers: req.Brokers, UseTLS: req.UseTLS,
		TLSSkipVerify: req.TLSSkipVerify, SASLMechanism: req.SASLMechanism,
		Username: req.Username, Password: req.Password, Selector: req.Selector,
		PublishChanges: req.PublishChanges, EnableWriteback: req.EnableWriteback,
		AutoCreateTopics: req.AutoCreateTopics, Enabled: req.Enabled,
		RequiredAcks: req.RequiredAcks, MaxRetries: req.MaxRetries,
		RetryBackoff: retryBackoff, ConsumerGroup: req.ConsumerGroup, WriteMaxAge: writeMaxAge,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"status": "created"})
}

func (h *handlers) handleUpdateKafka(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	var req kafkaAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var retryBackoff, writeMaxAge time.Duration
	if req.RetryBackoff != "" {
		if d, err := time.ParseDuration(req.RetryBackoff); err == nil {
			retryBackoff = d
		}
	}
	if req.WriteMaxAge != "" {
		if d, err := time.ParseDuration(req.WriteMaxAge); err == nil {
			writeMaxAge = d
		}
	}
	if err := h.engine.UpdateKafka(name, engine.KafkaUpdateRequest{
		Brokers: req.Brokers, UseTLS: req.UseTLS,
		TLSSkipVerify: req.TLSSkipVerify, SASLMechanism: req.SASLMechanism,
		Username: req.Username, Password: req.Password, Selector: req.Selector,
		PublishChanges: req.PublishChanges, EnableWriteback: req.EnableWriteback,
		AutoCreateTopics: req.AutoCreateTopics, Enabled: req.Enabled,
		RequiredAcks: req.RequiredAcks, MaxRetries: req.MaxRetries,
		RetryBackoff: retryBackoff, ConsumerGroup: req.ConsumerGroup, WriteMaxAge: writeMaxAge,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "updated"})
}

func (h *handlers) handleDeleteKafka(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.DeleteKafka(name); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "deleted"})
}

func (h *handlers) handleConnectKafka(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.ConnectKafka(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "connecting"})
}

func (h *handlers) handleDisconnectKafka(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	h.engine.DisconnectKafka(name)
	h.writeJSON(w, map[string]string{"status": "disconnected"})
}

// --- Triggers ---

type triggerAPIRequest struct {
	Name         string                  `json:"name"`
	Enabled      bool                    `json:"enabled"`
	PLC          string                  `json:"plc"`
	TriggerTag   string                  `json:"trigger_tag"`
	Condition    config.TriggerCondition `json:"condition"`
	AckTag       string                  `json:"ack_tag"`
	DebounceMS   int                     `json:"debounce_ms"`
	Tags         []string                `json:"tags"`
	MQTTBroker   string                  `json:"mqtt_broker"`
	KafkaCluster string                  `json:"kafka_cluster"`
	Selector     string                  `json:"selector"`
	Metadata     map[string]string       `json:"metadata"`
	PublishPack  string                  `json:"publish_pack"`
}

func (h *handlers) handleCreateTrigger(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req triggerAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.CreateTrigger(engine.TriggerCreateRequest{
		Name: req.Name, Enabled: req.Enabled, PLC: req.PLC,
		TriggerTag: req.TriggerTag, Condition: req.Condition, AckTag: req.AckTag,
		DebounceMS: req.DebounceMS, Tags: req.Tags, MQTTBroker: req.MQTTBroker,
		KafkaCluster: req.KafkaCluster, Selector: req.Selector,
		Metadata: req.Metadata, PublishPack: req.PublishPack,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"status": "created"})
}

func (h *handlers) handleUpdateTrigger(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	var req triggerAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.UpdateTrigger(name, engine.TriggerUpdateRequest{
		Enabled: req.Enabled, PLC: req.PLC,
		TriggerTag: req.TriggerTag, Condition: req.Condition, AckTag: req.AckTag,
		DebounceMS: req.DebounceMS, Tags: req.Tags, MQTTBroker: req.MQTTBroker,
		KafkaCluster: req.KafkaCluster, Selector: req.Selector,
		Metadata: req.Metadata, PublishPack: req.PublishPack,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "updated"})
}

func (h *handlers) handleDeleteTrigger(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.DeleteTrigger(name); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "deleted"})
}

func (h *handlers) handleStartTrigger(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.StartTrigger(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "started"})
}

func (h *handlers) handleStopTrigger(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	h.engine.StopTrigger(name)
	h.writeJSON(w, map[string]string{"status": "stopped"})
}

func (h *handlers) handleTestFireTrigger(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.TestFireTrigger(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "fired"})
}

// --- Push ---

type pushAPIRequest struct {
	Name            string                `json:"name"`
	Enabled         bool                  `json:"enabled"`
	Conditions      []config.PushCondition `json:"conditions"`
	URL             string                `json:"url"`
	Method          string                `json:"method"`
	ContentType     string                `json:"content_type"`
	Headers         map[string]string     `json:"headers"`
	Body            string                `json:"body"`
	Auth            config.PushAuthConfig `json:"auth"`
	CooldownMin     string                `json:"cooldown_min"`
	CooldownPerCond bool                  `json:"cooldown_per_condition"`
	Timeout         string                `json:"timeout"`
}

func (h *handlers) handleCreatePush(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req pushAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cooldown, timeout time.Duration
	if req.CooldownMin != "" {
		if d, err := time.ParseDuration(req.CooldownMin); err == nil {
			cooldown = d
		}
	}
	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = d
		}
	}
	if err := h.engine.CreatePush(engine.PushCreateRequest{
		Name: req.Name, Enabled: req.Enabled, Conditions: req.Conditions,
		URL: req.URL, Method: req.Method, ContentType: req.ContentType,
		Headers: req.Headers, Body: req.Body, Auth: req.Auth,
		CooldownMin: cooldown, CooldownPerCond: req.CooldownPerCond, Timeout: timeout,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"status": "created"})
}

func (h *handlers) handleUpdatePush(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	var req pushAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var cooldown, timeout time.Duration
	if req.CooldownMin != "" {
		if d, err := time.ParseDuration(req.CooldownMin); err == nil {
			cooldown = d
		}
	}
	if req.Timeout != "" {
		if d, err := time.ParseDuration(req.Timeout); err == nil {
			timeout = d
		}
	}
	if err := h.engine.UpdatePush(name, engine.PushUpdateRequest{
		Enabled: req.Enabled, Conditions: req.Conditions,
		URL: req.URL, Method: req.Method, ContentType: req.ContentType,
		Headers: req.Headers, Body: req.Body, Auth: req.Auth,
		CooldownMin: cooldown, CooldownPerCond: req.CooldownPerCond, Timeout: timeout,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "updated"})
}

func (h *handlers) handleDeletePush(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.DeletePush(name); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "deleted"})
}

func (h *handlers) handleStartPush(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.StartPush(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "started"})
}

func (h *handlers) handleStopPush(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	h.engine.StopPush(name)
	h.writeJSON(w, map[string]string{"status": "stopped"})
}

func (h *handlers) handleTestFirePush(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.TestFirePush(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "fired"})
}

// --- TagPacks ---

type tagPackAPIRequest struct {
	Name          string               `json:"name"`
	Enabled       bool                 `json:"enabled"`
	MQTTEnabled   bool                 `json:"mqtt_enabled"`
	KafkaEnabled  bool                 `json:"kafka_enabled"`
	ValkeyEnabled bool                 `json:"valkey_enabled"`
	Members       []config.TagPackMember `json:"members"`
}

func (h *handlers) handleCreateTagPack(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req tagPackAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.CreateTagPack(engine.TagPackCreateRequest{
		Name: req.Name, Enabled: req.Enabled,
		MQTTEnabled: req.MQTTEnabled, KafkaEnabled: req.KafkaEnabled,
		ValkeyEnabled: req.ValkeyEnabled, Members: req.Members,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"status": "created"})
}

func (h *handlers) handleUpdateTagPack(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	var req tagPackAPIRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.UpdateTagPack(name, engine.TagPackUpdateRequest{
		Enabled: req.Enabled,
		MQTTEnabled: req.MQTTEnabled, KafkaEnabled: req.KafkaEnabled,
		ValkeyEnabled: req.ValkeyEnabled, Members: req.Members,
	}); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "updated"})
}

func (h *handlers) handleDeleteTagPack(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.DeleteTagPack(name); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "deleted"})
}

func (h *handlers) handleToggleTagPack(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	enabled, err := h.engine.ToggleTagPack(name)
	if err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]bool{"enabled": enabled})
}

func (h *handlers) handleAddTagPackMember(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	var member config.TagPackMember
	if err := json.NewDecoder(r.Body).Decode(&member); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.AddTagPackMember(name, member); err != nil {
		h.writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"status": "added"})
}
