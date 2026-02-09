// Package tagpack provides Tag Pack functionality for grouping and publishing tags atomically.
package tagpack

import (
	"time"
)

// PackValue is the JSON structure published when a TagPack is triggered.
// Tags are stored in a flat map with "plc.tag" keys for easy access.
type PackValue struct {
	Name      string                 `json:"name"`
	Timestamp time.Time              `json:"timestamp"`
	Tags      map[string]TagData     `json:"tags"`           // "plc.tag" -> TagData
	PLCs      map[string]PLCMetadata `json:"plcs,omitempty"` // plc -> metadata (only if errors)
}

// TagData holds the value and metadata for a single tag.
// When a tag has an alias, the alias is used in the map key and the original
// tag name/address is stored in the MemLoc field.
// The map key format is "plc.tag" (e.g., "s7.test_wstring" or "logix_L7.Counter").
type TagData struct {
	Value  interface{} `json:"value"`
	Type   string      `json:"type"`
	PLC    string      `json:"plc"`               // PLC name for easy filtering
	MemLoc string      `json:"memloc,omitempty"` // Memory location (S7/Omron address) when alias is used
}

// PLCMetadata holds information about a PLC's connection state.
// Only included in output when there are connection issues.
type PLCMetadata struct {
	Address   string `json:"address"`
	Family    string `json:"family"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}
