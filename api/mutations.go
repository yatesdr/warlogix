package api

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"warlink/config"
	"warlink/engine"
)

func (h *handlers) writeEngineError(w http.ResponseWriter, err error) {
	h.writeError(w, engine.EngineHTTPStatus(err), err.Error())
}

func (h *handlers) requireEngine(w http.ResponseWriter) bool {
	if h.engine == nil {
		h.writeError(w, http.StatusServiceUnavailable, "mutation API not available")
		return false
	}
	return true
}

// --- PLC ---

func (h *handlers) handleCreatePLC(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req engine.PLCHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.CreatePLC(req.ToCreateRequest()); err != nil {
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
	var req engine.PLCHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.UpdatePLC(name, req.ToUpdateRequest()); err != nil {
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

func (h *handlers) handleCreateMQTT(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req engine.MQTTHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.CreateMQTT(req.ToCreateRequest()); err != nil {
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
	var req engine.MQTTHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.UpdateMQTT(name, req.ToUpdateRequest()); err != nil {
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

func (h *handlers) handleCreateValkey(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req engine.ValkeyHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.CreateValkey(req.ToCreateRequest()); err != nil {
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
	var req engine.ValkeyHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.UpdateValkey(name, req.ToUpdateRequest()); err != nil {
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

// kafkaAPIRequest wraps engine.KafkaHTTPRequest with a JSON array for brokers.
// The REST API accepts brokers as a JSON array, not comma-separated.
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

func (r kafkaAPIRequest) toHTTP() engine.KafkaHTTPRequest {
	return engine.KafkaHTTPRequest{
		Name: r.Name, BrokerList: r.Brokers, UseTLS: r.UseTLS,
		TLSSkipVerify: r.TLSSkipVerify, SASLMechanism: r.SASLMechanism,
		Username: r.Username, Password: r.Password, Selector: r.Selector,
		PublishChanges: r.PublishChanges, EnableWriteback: r.EnableWriteback,
		AutoCreateTopics: r.AutoCreateTopics, Enabled: r.Enabled,
		RequiredAcks: r.RequiredAcks, MaxRetries: r.MaxRetries,
		RetryBackoff: r.RetryBackoff, ConsumerGroup: r.ConsumerGroup, WriteMaxAge: r.WriteMaxAge,
	}
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
	if err := h.engine.CreateKafka(req.toHTTP().ToCreateRequest()); err != nil {
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
	if err := h.engine.UpdateKafka(name, req.toHTTP().ToUpdateRequest()); err != nil {
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

// --- Rules ---

func (h *handlers) handleListRules(w http.ResponseWriter, r *http.Request) {
	ruleMgr := h.managers.GetRuleMgr()
	if ruleMgr == nil {
		h.writeJSON(w, []interface{}{})
		return
	}
	h.writeJSON(w, ruleMgr.GetAllRuleInfo())
}

func (h *handlers) handleGetRule(w http.ResponseWriter, r *http.Request) {
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	cfg := h.managers.GetConfig()
	rc := cfg.FindRule(name)
	if rc == nil {
		h.writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	h.writeJSON(w, rc)
}

func (h *handlers) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req engine.RuleHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.CreateRule(req.ToCreateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	h.writeJSON(w, map[string]string{"status": "created"})
}

func (h *handlers) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	var req engine.RuleHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.UpdateRule(name, req.ToUpdateRequest()); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "updated"})
}

func (h *handlers) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.DeleteRule(name); err != nil {
		h.writeEngineError(w, err)
		return
	}
	h.writeJSON(w, map[string]string{"status": "deleted"})
}

func (h *handlers) handleStartRule(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.StartRule(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "started"})
}

func (h *handlers) handleStopRule(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	h.engine.StopRule(name)
	h.writeJSON(w, map[string]string{"status": "stopped"})
}

func (h *handlers) handleTestFireRule(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	name, _ := url.PathUnescape(chi.URLParam(r, "name"))
	if err := h.engine.TestFireRule(name); err != nil {
		h.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.writeJSON(w, map[string]string{"status": "fired"})
}

// --- TagPacks ---

func (h *handlers) handleCreateTagPack(w http.ResponseWriter, r *http.Request) {
	if !h.requireEngine(w) {
		return
	}
	var req engine.TagPackHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.CreateTagPack(req.ToCreateRequest()); err != nil {
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
	var req engine.TagPackHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := h.engine.UpdateTagPack(name, req.ToUpdateRequest()); err != nil {
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
