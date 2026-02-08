// Package namespace provides utilities for constructing topic and key paths
// with consistent namespace prefixing across all services (MQTT, Valkey, Kafka).
package namespace

import "strings"

// Builder constructs namespace-prefixed topics and keys.
type Builder struct {
	namespace string
	selector  string
}

// New creates a new namespace builder.
func New(namespace, selector string) *Builder {
	return &Builder{
		namespace: namespace,
		selector:  selector,
	}
}

// --- MQTT (delimiter: /) ---

// MQTTTagTopic returns the topic for a tag value: {ns}[/{sel}]/{plc}/tags/{tag}
func (b *Builder) MQTTTagTopic(plc, tag string) string {
	return b.mqttBase() + "/" + plc + "/tags/" + tag
}

// MQTTHealthTopic returns the topic for health status: {ns}[/{sel}]/{plc}/health
func (b *Builder) MQTTHealthTopic(plc string) string {
	return b.mqttBase() + "/" + plc + "/health"
}

// MQTTWriteTopic returns the topic for write requests: {ns}[/{sel}]/{plc}/write
func (b *Builder) MQTTWriteTopic(plc string) string {
	return b.mqttBase() + "/" + plc + "/write"
}

// MQTTWriteResponseTopic returns the topic for write responses: {ns}[/{sel}]/{plc}/write/response
func (b *Builder) MQTTWriteResponseTopic(plc string) string {
	return b.mqttBase() + "/" + plc + "/write/response"
}

// MQTTPackTopic returns the topic for a TagPack: {ns}/packs/{pack}
func (b *Builder) MQTTPackTopic(pack string) string {
	return b.namespace + "/packs/" + pack
}

// MQTTTriggerTopic returns the topic for a trigger message: {ns}[/{sel}]/triggers/{trigger}
func (b *Builder) MQTTTriggerTopic(trigger string) string {
	return b.mqttBase() + "/triggers/" + trigger
}

// MQTTBase returns the base topic for JSON messages: {ns}[/{sel}]
func (b *Builder) MQTTBase() string {
	return b.mqttBase()
}

func (b *Builder) mqttBase() string {
	if b.selector != "" {
		return b.namespace + "/" + b.selector
	}
	return b.namespace
}

// --- Valkey (delimiter: :) ---

// ValkeyTagKey returns the key for a tag value: {ns}[:{sel}]:{plc}:tags:{tag}
func (b *Builder) ValkeyTagKey(plc, tag string) string {
	return b.valkeyBase() + ":" + plc + ":tags:" + tag
}

// ValkeyHealthKey returns the key for health status: {ns}[:{sel}]:{plc}:health
func (b *Builder) ValkeyHealthKey(plc string) string {
	return b.valkeyBase() + ":" + plc + ":health"
}

// ValkeyChangesChannel returns the channel for PLC changes: {ns}[:{sel}]:{plc}:changes
func (b *Builder) ValkeyChangesChannel(plc string) string {
	return b.valkeyBase() + ":" + plc + ":changes"
}

// ValkeyAllChangesChannel returns the channel for all changes: {ns}[:{sel}]:_all:changes
func (b *Builder) ValkeyAllChangesChannel() string {
	return b.valkeyBase() + ":_all:changes"
}

// ValkeyWriteQueue returns the queue key for write requests: {ns}[:{sel}]:writes
func (b *Builder) ValkeyWriteQueue() string {
	return b.valkeyBase() + ":writes"
}

// ValkeyWriteResponseChannel returns the channel for write responses: {ns}[:{sel}]:write:responses
func (b *Builder) ValkeyWriteResponseChannel() string {
	return b.valkeyBase() + ":write:responses"
}

// ValkeyPackChannel returns the channel for a TagPack: {ns}:packs:{pack}
func (b *Builder) ValkeyPackChannel(pack string) string {
	return b.namespace + ":packs:" + pack
}

// ValkeyFactory returns the factory identifier for JSON messages: {ns}[:{sel}]
func (b *Builder) ValkeyFactory() string {
	return b.valkeyBase()
}

func (b *Builder) valkeyBase() string {
	if b.selector != "" {
		return b.namespace + ":" + b.selector
	}
	return b.namespace
}

// --- Kafka (delimiter: - for topics, . for health) ---

// KafkaTagTopic returns the topic for tag values: {ns}[-{sel}]
func (b *Builder) KafkaTagTopic() string {
	return b.kafkaBase()
}

// KafkaHealthTopic returns the topic for health status: {ns}[-{sel}].health
func (b *Builder) KafkaHealthTopic() string {
	return b.kafkaBase() + ".health"
}

// KafkaWriteTopic returns the topic for write requests: {ns}[-{sel}]-writes
func (b *Builder) KafkaWriteTopic() string {
	return b.kafkaBase() + "-writes"
}

// KafkaWriteResponseTopic returns the topic for write responses: {ns}[-{sel}]-write-responses
func (b *Builder) KafkaWriteResponseTopic() string {
	return b.kafkaBase() + "-write-responses"
}

// KafkaPackTopic returns the topic for a TagPack: {ns}-packs-{pack}
func (b *Builder) KafkaPackTopic(pack string) string {
	return b.namespace + "-packs-" + sanitizeKafkaTopic(pack)
}

// KafkaTriggerTopic returns the topic for trigger messages: {ns}[-{sel}]-triggers
func (b *Builder) KafkaTriggerTopic() string {
	return b.kafkaBase() + "-triggers"
}

func (b *Builder) kafkaBase() string {
	if b.selector != "" {
		return b.namespace + "-" + b.selector
	}
	return b.namespace
}

// sanitizeKafkaTopic removes or replaces characters not allowed in Kafka topic names.
// Kafka allows: a-z, A-Z, 0-9, ., _, -
func sanitizeKafkaTopic(s string) string {
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			result.WriteRune(r)
		} else {
			result.WriteRune('-')
		}
	}
	return result.String()
}
