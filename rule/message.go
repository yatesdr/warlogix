package rule

import (
	"encoding/json"
	"sync/atomic"
	"time"
)

// Global sequence counter for message ordering.
var globalSequence uint64

// Message represents a rule event message to be sent to Kafka/MQTT.
type Message struct {
	Rule      string                 `json:"rule"`
	Timestamp string                 `json:"timestamp"`
	Sequence  uint64                 `json:"sequence"`
	PLC       string                 `json:"plc,omitempty"`
	Trigger   map[string]interface{} `json:"trigger,omitempty"` // Condition tag+value that fired
	Data      map[string]interface{} `json:"data"`
}

// NewMessage creates a new rule message with captured data.
func NewMessage(ruleName, plcName string, triggerInfo map[string]interface{}, data map[string]interface{}) *Message {
	if data == nil {
		data = make(map[string]interface{})
	}
	return &Message{
		Rule:      ruleName,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Sequence:  atomic.AddUint64(&globalSequence, 1),
		PLC:       plcName,
		Trigger:   triggerInfo,
		Data:      data,
	}
}

// ToJSON serializes the message to JSON bytes.
func (m *Message) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// Key returns the rule name for Kafka partitioning.
func (m *Message) Key() []byte {
	return []byte(m.Rule)
}
