package s7

import (
	"encoding/binary"
	"fmt"
	"math"
)

// TagValue represents the result of reading an S7 address with type conversion helpers.
type TagValue struct {
	Name     string // Address string as requested (e.g., "DB1.DBW0")
	DataType uint16 // S7 data type code
	Bytes    []byte // Raw value bytes (big-endian, as S7 uses)
	BitNum   int    // Bit number for BOOL types (-1 for non-bit)
	Error    error  // Per-tag error (nil if successful)
}

// Bool returns the tag value as a boolean.
// Works for BOOL type or extracts a specific bit from a byte.
func (v *TagValue) Bool() (bool, error) {
	if v.Error != nil {
		return false, v.Error
	}
	if len(v.Bytes) < 1 {
		return false, fmt.Errorf("insufficient data for BOOL")
	}

	if v.BitNum >= 0 && v.BitNum <= 7 {
		// Extract specific bit
		return (v.Bytes[0] & (1 << v.BitNum)) != 0, nil
	}

	// Non-bit BOOL (full byte)
	return v.Bytes[0] != 0, nil
}

// Int returns the tag value as a signed 64-bit integer.
// Works for SINT, INT, DINT, and LINT types.
func (v *TagValue) Int() (int64, error) {
	if v.Error != nil {
		return 0, v.Error
	}

	switch v.DataType {
	case TypeSInt:
		if len(v.Bytes) < 1 {
			return 0, fmt.Errorf("insufficient data for SINT")
		}
		return int64(int8(v.Bytes[0])), nil
	case TypeInt:
		if len(v.Bytes) < 2 {
			return 0, fmt.Errorf("insufficient data for INT")
		}
		return int64(int16(binary.BigEndian.Uint16(v.Bytes))), nil
	case TypeDInt:
		if len(v.Bytes) < 4 {
			return 0, fmt.Errorf("insufficient data for DINT")
		}
		return int64(int32(binary.BigEndian.Uint32(v.Bytes))), nil
	case TypeLInt:
		if len(v.Bytes) < 8 {
			return 0, fmt.Errorf("insufficient data for LINT")
		}
		return int64(binary.BigEndian.Uint64(v.Bytes)), nil
	default:
		return 0, fmt.Errorf("type mismatch: expected signed integer, got %s", v.TypeName())
	}
}

// Uint returns the tag value as an unsigned 64-bit integer.
// Works for BYTE, WORD, DWORD, and ULINT types.
func (v *TagValue) Uint() (uint64, error) {
	if v.Error != nil {
		return 0, v.Error
	}

	switch v.DataType {
	case TypeByte:
		if len(v.Bytes) < 1 {
			return 0, fmt.Errorf("insufficient data for BYTE")
		}
		return uint64(v.Bytes[0]), nil
	case TypeWord:
		if len(v.Bytes) < 2 {
			return 0, fmt.Errorf("insufficient data for WORD")
		}
		return uint64(binary.BigEndian.Uint16(v.Bytes)), nil
	case TypeDWord:
		if len(v.Bytes) < 4 {
			return 0, fmt.Errorf("insufficient data for DWORD")
		}
		return uint64(binary.BigEndian.Uint32(v.Bytes)), nil
	case TypeULInt:
		if len(v.Bytes) < 8 {
			return 0, fmt.Errorf("insufficient data for ULINT")
		}
		return binary.BigEndian.Uint64(v.Bytes), nil
	default:
		return 0, fmt.Errorf("type mismatch: expected unsigned integer, got %s", v.TypeName())
	}
}

// Float returns the tag value as a 64-bit float.
// Works for REAL (float32) and LREAL (float64) types.
func (v *TagValue) Float() (float64, error) {
	if v.Error != nil {
		return 0, v.Error
	}

	switch v.DataType {
	case TypeReal:
		if len(v.Bytes) < 4 {
			return 0, fmt.Errorf("insufficient data for REAL")
		}
		bits := binary.BigEndian.Uint32(v.Bytes)
		return float64(math.Float32frombits(bits)), nil
	case TypeLReal:
		if len(v.Bytes) < 8 {
			return 0, fmt.Errorf("insufficient data for LREAL")
		}
		bits := binary.BigEndian.Uint64(v.Bytes)
		return math.Float64frombits(bits), nil
	default:
		return 0, fmt.Errorf("type mismatch: expected float, got %s", v.TypeName())
	}
}

// GoValue returns the tag value converted to an appropriate Go type.
// Returns nil if there's an error.
func (v *TagValue) GoValue() interface{} {
	if v.Error != nil {
		return nil
	}

	switch v.DataType {
	case TypeBool:
		if val, err := v.Bool(); err == nil {
			return val
		}
	case TypeSInt, TypeInt, TypeDInt, TypeLInt:
		if val, err := v.Int(); err == nil {
			return val
		}
	case TypeByte, TypeWord, TypeDWord, TypeULInt:
		if val, err := v.Uint(); err == nil {
			return val
		}
	case TypeReal, TypeLReal:
		if val, err := v.Float(); err == nil {
			return val
		}
	}

	// For unknown types, return as []int for JSON compatibility
	return v.bytesToIntArray()
}

// bytesToIntArray converts the raw bytes to []int for JSON-friendly output.
func (v *TagValue) bytesToIntArray() []int {
	intBytes := make([]int, len(v.Bytes))
	for i, b := range v.Bytes {
		intBytes[i] = int(b)
	}
	return intBytes
}

// TypeName returns the human-readable type name for this tag.
func (v *TagValue) TypeName() string {
	return TypeName(v.DataType)
}
