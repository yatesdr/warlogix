package engine

import "time"

// EventType identifies the kind of event emitted by the Engine.
type EventType int

const (
	// PLC events
	EventPLCCreated EventType = iota + 1
	EventPLCUpdated
	EventPLCDeleted
	EventPLCConnected
	EventPLCDisconnected

	// Tag events
	EventTagUpdated
	EventTagCreated
	EventTagDeleted
	EventTagWritten
	EventTagRead

	// MQTT events
	EventMQTTCreated
	EventMQTTUpdated
	EventMQTTDeleted
	EventMQTTStarted
	EventMQTTStopped

	// Valkey events
	EventValkeyCreated
	EventValkeyUpdated
	EventValkeyDeleted
	EventValkeyStarted
	EventValkeyStopped

	// Kafka events
	EventKafkaCreated
	EventKafkaUpdated
	EventKafkaDeleted
	EventKafkaConnected
	EventKafkaDisconnected

	// Rule events
	EventRuleCreated
	EventRuleUpdated
	EventRuleDeleted
	EventRuleStarted
	EventRuleStopped
	EventRuleTestFired

	// TagPack events
	EventTagPackCreated
	EventTagPackUpdated
	EventTagPackDeleted
	EventTagPackToggled
	EventTagPackServiceToggled
	EventTagPackMemberAdded
	EventTagPackMemberRemoved
	EventTagPackMemberIgnoreToggled

	// System events
	EventNamespaceChanged
	EventAPIToggled
	EventForcePublished
)

// Event is the envelope emitted by the Engine's EventBus.
type Event struct {
	Type      EventType
	Timestamp time.Time
	Payload   interface{}
}

// PLCEvent is the payload for PLC lifecycle events.
type PLCEvent struct {
	Name string
}

// TagEvent is the payload for tag mutation events.
type TagEvent struct {
	PLCName string
	TagName string
}

// ServiceEvent is the payload for MQTT/Valkey/Kafka lifecycle events.
type ServiceEvent struct {
	Name string
}

// RuleEvent is the payload for rule lifecycle events.
type RuleEvent struct {
	Name string
}

// TagPackEvent is the payload for TagPack lifecycle events.
type TagPackEvent struct {
	Name string
}

// TagPackServiceEvent is the payload for TagPack service toggle events.
type TagPackServiceEvent struct {
	Name    string
	Service string // "mqtt", "kafka", "valkey"
	Enabled bool
}

// TagPackMemberEvent is the payload for TagPack member add/remove events.
type TagPackMemberEvent struct {
	PackName string
	Index    int
}

// SystemEvent is the payload for system-level events.
type SystemEvent struct {
	Detail string
}
