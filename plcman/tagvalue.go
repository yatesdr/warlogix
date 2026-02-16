package plcman

import (
	"github.com/yatesdr/plcio/ads"
	"github.com/yatesdr/plcio/driver"
	"github.com/yatesdr/plcio/logix"
	"github.com/yatesdr/plcio/omron"
	"github.com/yatesdr/plcio/s7"
)

// TagValue is a unified wrapper that holds tag data from any PLC family.
// It stores pre-computed Go values and type information for display.
type TagValue struct {
	Name        string      // Tag name
	DataType    uint16      // Native type code (Logix or S7)
	Family      string      // PLC family ("logix", "s7", "beckhoff", etc.)
	Value       interface{} // Pre-computed Go value from GoValue()
	StableValue interface{} // Value with ignored members removed (for change detection)
	Bytes       []byte      // Original raw bytes (native byte order)
	Count       int         // Number of elements (1 for scalar, >1 for array)
	Error       error       // Per-tag error (nil if successful)
}

// GoValue returns the pre-computed Go value.
// For S7 and Logix tags, this was computed using the appropriate
// package's parsing logic with native byte order.
func (v *TagValue) GoValue() interface{} {
	if v.Error != nil {
		return nil
	}
	return v.Value
}

// TypeName returns the human-readable type name using the appropriate
// package's type naming based on the PLC family.
func (v *TagValue) TypeName() string {
	switch v.Family {
	case "s7":
		return s7.TypeName(v.DataType)
	case "beckhoff", "ads":
		return ads.TypeName(v.DataType)
	case "omron":
		return omron.TypeName(v.DataType)
	default:
		return logix.TypeName(v.DataType)
	}
}

// FromLogixTagValueDecoded creates a unified TagValue with UDT decoding support.
// If the tag is a structure type and client is provided, the value will be decoded
// as a map[string]interface{} with member names as keys.
func FromLogixTagValueDecoded(lv *logix.TagValue, client *logix.Client) *TagValue {
	if lv == nil {
		return nil
	}

	// Use decoded value for structures when client is available
	var value interface{}
	if client != nil {
		value = lv.GoValueDecoded(client)
	} else {
		value = lv.GoValue()
	}

	return &TagValue{
		Name:        lv.Name,
		DataType:    lv.DataType,
		Family:      "logix",
		Value:       value,
		StableValue: value, // Will be updated by SetIgnoreList if needed
		Bytes:       lv.Bytes,
		Count:       lv.Count,
		Error:       lv.Error,
	}
}

// ComputeStableValue returns a copy of the value with ignored members removed.
// For map values (decoded UDTs), this filters out keys in the ignore list.
// For other value types, returns the value unchanged.
func ComputeStableValue(value interface{}, ignoreList []string) interface{} {
	if len(ignoreList) == 0 {
		return value
	}

	// Check if value is a map (decoded UDT)
	mapVal, ok := value.(map[string]interface{})
	if !ok {
		return value
	}

	// Create ignore set for O(1) lookup
	ignoreSet := make(map[string]bool, len(ignoreList))
	for _, name := range ignoreList {
		ignoreSet[name] = true
	}

	// Create filtered copy
	filtered := make(map[string]interface{}, len(mapVal))
	for key, val := range mapVal {
		if !ignoreSet[key] {
			filtered[key] = val
		}
	}

	return filtered
}

// SetIgnoreList computes and sets the StableValue based on the ignore list.
// This should be called after the TagValue is created to filter out volatile members.
func (v *TagValue) SetIgnoreList(ignoreList []string) {
	if v == nil {
		return
	}
	v.StableValue = ComputeStableValue(v.Value, ignoreList)
}

// FromDriverTagValue creates a unified TagValue from a driver.TagValue.
// This is used when reading via the driver package's unified interface.
func FromDriverTagValue(dv *driver.TagValue) *TagValue {
	if dv == nil {
		return nil
	}
	return &TagValue{
		Name:        dv.Name,
		DataType:    dv.DataType,
		Family:      dv.Family,
		Value:       dv.Value,
		StableValue: dv.StableValue,
		Bytes:       dv.Bytes,
		Count:       dv.Count,
		Error:       dv.Error,
	}
}
