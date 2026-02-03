package trigger

import (
	"encoding/json"
	"sync/atomic"
	"time"
)

// Global sequence counter for message ordering
var globalSequence uint64

// Message represents a trigger event message to be sent to Kafka.
type Message struct {
	Trigger   string                 `json:"trigger"`
	Timestamp string                 `json:"timestamp"`
	Sequence  uint64                 `json:"sequence"`
	PLC       string                 `json:"plc"`
	Metadata  map[string]string      `json:"metadata,omitempty"`
	Data      map[string]interface{} `json:"data"`
}

// NewMessage creates a new trigger message with captured data.
func NewMessage(triggerName, plcName string, metadata map[string]string, data map[string]interface{}) *Message {
	return &Message{
		Trigger:   triggerName,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Sequence:  atomic.AddUint64(&globalSequence, 1),
		PLC:       plcName,
		Metadata:  metadata,
		Data:      data,
	}
}

// ToJSON serializes the message to JSON bytes.
func (m *Message) ToJSON() ([]byte, error) {
	return json.Marshal(m)
}

// Key returns a key for Kafka partitioning (trigger name + PLC).
func (m *Message) Key() []byte {
	return []byte(m.PLC + ":" + m.Trigger)
}
