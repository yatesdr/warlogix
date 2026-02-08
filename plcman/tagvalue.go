package plcman

import (
	"warlogix/ads"
	"warlogix/config"
	"warlogix/driver"
	"warlogix/logix"
	"warlogix/omron"
	"warlogix/s7"
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

// FromLogixTagValue creates a unified TagValue from a logix.TagValue.
// For UDT decoding with member names, use FromLogixTagValueDecoded instead.
func FromLogixTagValue(lv *logix.TagValue) *TagValue {
	if lv == nil {
		return nil
	}
	value := lv.GoValue()
	return &TagValue{
		Name:        lv.Name,
		DataType:    lv.DataType,
		Family:      "logix",
		Value:       value,
		StableValue: value,
		Bytes:       lv.Bytes,
		Count:       lv.Count,
		Error:       lv.Error,
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

// FromS7TagValue creates a unified TagValue from an s7.TagValue.
// This calls s7.TagValue.GoValue() which uses big-endian parsing.
func FromS7TagValue(sv *s7.TagValue, family config.PLCFamily) *TagValue {
	if sv == nil {
		return nil
	}
	dataType := sv.DataType
	if sv.Count > 1 {
		dataType = s7.MakeArrayType(dataType)
	}
	value := sv.GoValue() // Uses big-endian (native S7 format)
	return &TagValue{
		Name:        sv.Name,
		DataType:    dataType,
		Family:      "s7",
		Value:       value,
		StableValue: value,
		Bytes:       sv.Bytes,
		Count:       sv.Count,
		Error:       sv.Error,
	}
}

// ToLogixTagValue converts the unified TagValue back to a logix.TagValue.
// This is needed for compatibility with existing code that expects logix.TagValue.
// Note: The bytes are stored in their original native byte order.
func (v *TagValue) ToLogixTagValue() *logix.TagValue {
	if v == nil {
		return nil
	}
	return &logix.TagValue{
		Name:     v.Name,
		DataType: v.DataType,
		Bytes:    v.Bytes,
		Count:    v.Count,
		Error:    v.Error,
	}
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

// ToDriverTagValue converts the unified TagValue to a driver.TagValue.
func (v *TagValue) ToDriverTagValue() *driver.TagValue {
	if v == nil {
		return nil
	}
	return &driver.TagValue{
		Name:        v.Name,
		DataType:    v.DataType,
		Family:      v.Family,
		Value:       v.Value,
		StableValue: v.StableValue,
		Bytes:       v.Bytes,
		Count:       v.Count,
		Error:       v.Error,
	}
}

// LogixTagInfoToDriver converts a logix.TagInfo to a driver.TagInfo.
func LogixTagInfoToDriver(t logix.TagInfo) driver.TagInfo {
	dims := make([]uint32, len(t.Dimensions))
	for i, d := range t.Dimensions {
		dims[i] = uint32(d)
	}
	return driver.TagInfo{
		Name:       t.Name,
		TypeCode:   t.TypeCode,
		Instance:   t.Instance,
		Dimensions: dims,
		TypeName:   t.TypeName(),
		Writable:   t.IsReadable(),
	}
}

// DriverTagInfoToLogix converts a driver.TagInfo to a logix.TagInfo.
func DriverTagInfoToLogix(t driver.TagInfo) logix.TagInfo {
	dims := make([]int, len(t.Dimensions))
	for i, d := range t.Dimensions {
		dims[i] = int(d)
	}
	return logix.TagInfo{
		Name:       t.Name,
		TypeCode:   t.TypeCode,
		Instance:   t.Instance,
		Dimensions: dims,
	}
}
