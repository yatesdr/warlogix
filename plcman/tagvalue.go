package plcman

import (
	"warlogix/config"
	"warlogix/logix"
	"warlogix/s7"
)

// TagValue is a unified wrapper that holds tag data from any PLC family.
// It stores pre-computed Go values and type information for display.
type TagValue struct {
	Name     string      // Tag name
	DataType uint16      // Native type code (Logix or S7)
	Family   string      // PLC family ("logix", "s7", "beckhoff", etc.)
	Value    interface{} // Pre-computed Go value from GoValue()
	Bytes    []byte      // Original raw bytes (native byte order)
	Count    int         // Number of elements (1 for scalar, >1 for array)
	Error    error       // Per-tag error (nil if successful)
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
	default:
		return logix.TypeName(v.DataType)
	}
}

// FromLogixTagValue creates a unified TagValue from a logix.TagValue.
func FromLogixTagValue(lv *logix.TagValue) *TagValue {
	if lv == nil {
		return nil
	}
	return &TagValue{
		Name:     lv.Name,
		DataType: lv.DataType,
		Family:   "logix",
		Value:    lv.GoValue(),
		Bytes:    lv.Bytes,
		Count:    lv.Count,
		Error:    lv.Error,
	}
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
	return &TagValue{
		Name:     sv.Name,
		DataType: dataType,
		Family:   "s7",
		Value:    sv.GoValue(), // Uses big-endian (native S7 format)
		Bytes:    sv.Bytes,
		Count:    sv.Count,
		Error:    sv.Error,
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
