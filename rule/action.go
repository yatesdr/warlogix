package rule

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"warlink/config"
	"warlink/namespace"
)

// tagRefRegex matches #PLCName.tagName references in body templates.
var tagRefRegex = regexp.MustCompile(`#([a-zA-Z_]\w*(?:\.\w+)+)`)

// executeAction dispatches a single action by type.
func (r *Rule) executeAction(action *config.RuleAction) {
	switch action.Type {
	case config.ActionPublish:
		r.executePublish(action)
	case config.ActionWebhook:
		r.executeWebhook(action)
	case config.ActionWriteback:
		r.executeWriteback(action)
	default:
		r.log("unknown action type: %s", action.Type)
	}
}

// executePublish reads tag/pack data and publishes to MQTT and/or Kafka.
func (r *Rule) executePublish(action *config.RuleAction) {
	r.mu.RLock()
	packMgr := r.packMgr
	ns := r.namespace
	r.mu.RUnlock()

	if ns == "" {
		r.log("publish: no namespace configured, skipping")
		return
	}

	var data map[string]interface{}

	if action.TagOrPack != "" {
		if strings.HasPrefix(action.TagOrPack, "pack:") {
			// Read TagPack
			packName := strings.TrimPrefix(action.TagOrPack, "pack:")
			if packMgr != nil {
				pv := packMgr.GetPackValue(packName)
				if pv != nil {
					data = map[string]interface{}{packName: pv.Tags}
				} else {
					r.log("publish: pack '%s' not found", packName)
					data = make(map[string]interface{})
				}
			}
		} else {
			// Read single tag — use first condition's PLC
			plcName := ""
			if len(r.config.Conditions) > 0 {
				plcName = r.config.Conditions[0].PLC
			}
			if plcName != "" {
				value, err := r.reader.ReadTag(plcName, action.TagOrPack)
				if err != nil {
					r.log("publish: error reading tag %s.%s: %v", plcName, action.TagOrPack, err)
					data = make(map[string]interface{})
				} else {
					data = map[string]interface{}{action.TagOrPack: value}
				}
			}
		}
	}
	if data == nil {
		data = make(map[string]interface{})
	}

	// Build trigger info if requested
	var triggerInfo map[string]interface{}
	if action.IncludeTrigger && len(r.config.Conditions) > 0 {
		triggerInfo = make(map[string]interface{})
		for _, cond := range r.config.Conditions {
			key := cond.PLC + "." + cond.Tag
			value, err := r.reader.ReadTag(cond.PLC, cond.Tag)
			if err == nil {
				triggerInfo[key] = value
			}
		}
	}

	// Build message
	plcName := ""
	if len(r.config.Conditions) > 0 {
		plcName = r.config.Conditions[0].PLC
	}
	msg := NewMessage(r.config.Name, plcName, triggerInfo, data)
	jsonData, err := msg.ToJSON()
	if err != nil {
		r.log("publish: JSON marshal error: %v", err)
		return
	}

	builder := namespace.New(ns, "")

	// Publish to MQTT
	if r.mqtt != nil && !strings.EqualFold(action.MQTTBroker, "none") {
		topic := action.MQTTTopic
		if topic == "" {
			topic = "rules/" + r.config.Name
		}
		mqttTopic := builder.MQTTRuleTopic(topic)
		brokers := r.getMQTTBrokers(action.MQTTBroker)
		for _, broker := range brokers {
			if r.mqtt.PublishRawQoS2ToBroker(broker, mqttTopic, jsonData) {
				r.log("publish: sent to MQTT broker '%s', topic=%s", broker, mqttTopic)
			} else {
				r.log("publish: failed to send to MQTT broker '%s'", broker)
			}
		}
	}

	// Publish to Kafka
	if r.kafka != nil && !strings.EqualFold(action.KafkaCluster, "none") {
		kafkaTopic := action.KafkaTopic
		if kafkaTopic == "" {
			kafkaTopic = "rules"
		}
		fullTopic := builder.KafkaRuleTopic(kafkaTopic)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		clusters := r.getKafkaClusters(action.KafkaCluster)
		for _, cluster := range clusters {
			if err := r.kafka.ProduceWithRetry(ctx, cluster, fullTopic, msg.Key(), jsonData); err != nil {
				r.log("publish: failed to send to Kafka cluster '%s': %v", cluster, err)
			} else {
				r.log("publish: sent to Kafka cluster '%s', topic=%s", cluster, fullTopic)
			}
		}
		cancel()
	}
}

// executeWebhook sends an HTTP request with template-resolved body.
func (r *Rule) executeWebhook(action *config.RuleAction) {
	if action.URL == "" {
		r.log("webhook: no URL configured, skipping")
		return
	}

	// Resolve body template
	body := r.resolveBody(action.Body)

	// Build HTTP request
	method := action.Method
	if method == "" {
		method = "POST"
	}

	var bodyReader io.Reader
	if body != "" {
		bodyReader = bytes.NewBufferString(body)
	}

	req, err := http.NewRequest(method, action.URL, bodyReader)
	if err != nil {
		r.log("webhook: failed to build request: %v", err)
		return
	}

	// Set Content-Type
	ct := action.ContentType
	if ct == "" {
		ct = "application/json"
	}
	if body != "" {
		req.Header.Set("Content-Type", ct)
	}

	// Set custom headers
	for k, v := range action.Headers {
		req.Header.Set(k, v)
	}

	// Apply auth
	switch action.Auth.Type {
	case config.RuleAuthBearer, config.RuleAuthJWT:
		req.Header.Set("Authorization", "Bearer "+action.Auth.Token)
	case config.RuleAuthBasic:
		req.SetBasicAuth(action.Auth.Username, action.Auth.Password)
	case config.RuleAuthCustomHeader:
		if action.Auth.HeaderName != "" {
			req.Header.Set(action.Auth.HeaderName, action.Auth.HeaderValue)
		}
	}

	// Use action timeout or default
	client := r.httpClient
	if action.Timeout > 0 {
		client = &http.Client{Timeout: action.Timeout}
	}

	resp, err := client.Do(req)
	if err != nil {
		r.log("webhook: HTTP request failed: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	name := action.Name
	if name == "" {
		name = action.URL
	}
	r.log("webhook: sent %s to %s, status=%d", method, name, resp.StatusCode)
}

// executeWriteback writes a value to a PLC tag.
func (r *Rule) executeWriteback(action *config.RuleAction) {
	if r.writer == nil {
		r.log("writeback: no tag writer available")
		return
	}

	plc := action.WritePLC
	if plc == "" && len(r.config.Conditions) > 0 {
		plc = r.config.Conditions[0].PLC
	}
	if plc == "" {
		r.log("writeback: no PLC specified")
		return
	}

	if action.WriteTag == "" {
		r.log("writeback: no tag specified")
		return
	}

	if err := r.writer.WriteTag(plc, action.WriteTag, action.WriteValue); err != nil {
		r.log("writeback: failed to write %s.%s: %v", plc, action.WriteTag, err)
	} else {
		r.log("writeback: wrote %v to %s.%s", action.WriteValue, plc, action.WriteTag)
	}
}

// resolveBody replaces #PLC.tagName references in the body template with live values.
func (r *Rule) resolveBody(body string) string {
	if body == "" {
		return ""
	}

	return tagRefRegex.ReplaceAllStringFunc(body, func(match string) string {
		ref := match[1:]
		dotIdx := strings.IndexByte(ref, '.')
		if dotIdx < 0 {
			return match
		}
		plcName := ref[:dotIdx]
		tagPath := ref[dotIdx+1:]

		value, err := r.reader.ReadTag(plcName, tagPath)
		if err != nil {
			return match
		}
		return fmt.Sprintf("%v", value)
	})
}

// getMQTTBrokers returns brokers to publish to.
func (r *Rule) getMQTTBrokers(broker string) []string {
	if r.mqtt == nil {
		return nil
	}
	if broker == "" || strings.EqualFold(broker, "all") {
		return r.mqtt.ListBrokers()
	}
	return []string{broker}
}

// getKafkaClusters returns clusters to publish to.
func (r *Rule) getKafkaClusters(cluster string) []string {
	if cluster == "" || strings.EqualFold(cluster, "all") {
		return r.kafka.ListClusters()
	}
	return []string{cluster}
}
