// Package fins provides Omron FINS protocol communication for Omron PLCs.
// FINS (Factory Interface Network Service) is Omron's industrial protocol.
// This package uses big-endian byte order (native FINS/Omron format).
package fins

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Memory area codes for FINS protocol.
// These match the Omron FINS specification.
const (
	// Bit areas (for bit-level access)
	MemoryAreaCIOBit  byte = 0x30 // CIO area; bit
	MemoryAreaWRBit   byte = 0x31 // Work area; bit
	MemoryAreaHRBit   byte = 0x32 // Holding area; bit
	MemoryAreaARBit   byte = 0x33 // Auxiliary area; bit
	MemoryAreaDMBit   byte = 0x02 // Data memory area; bit
	MemoryAreaTaskBit byte = 0x06 // Task flags; bit

	// Word areas (for word-level access)
	MemoryAreaCIOWord byte = 0xB0 // CIO area; word
	MemoryAreaWRWord  byte = 0xB1 // Work area; word
	MemoryAreaHRWord  byte = 0xB2 // Holding area; word
	MemoryAreaARWord  byte = 0xB3 // Auxiliary area; word
	MemoryAreaDMWord  byte = 0x82 // Data memory area; word

	// Timer/Counter areas
	MemoryAreaTimerCounterPV byte = 0x89 // Timer/Counter PV
)

// Data type codes for Omron FINS.
// These are internal type codes for the warlogix system.
const (
	TypeVoid   uint16 = 0x00
	TypeBool   uint16 = 0x01 // BOOL (1 bit, read as word)
	TypeByte   uint16 = 0x02 // BYTE/USINT (1 byte)
	TypeSByte  uint16 = 0x03 // SINT (1 byte signed)
	TypeWord   uint16 = 0x04 // WORD/UINT (2 bytes unsigned)
	TypeInt16  uint16 = 0x05 // INT (2 bytes signed)
	TypeDWord  uint16 = 0x06 // DWORD/UDINT (4 bytes unsigned)
	TypeInt32  uint16 = 0x07 // DINT (4 bytes signed)
	TypeLWord  uint16 = 0x08 // LWORD/ULINT (8 bytes unsigned)
	TypeInt64  uint16 = 0x09 // LINT (8 bytes signed)
	TypeReal   uint16 = 0x0A // REAL (4 bytes float)
	TypeLReal  uint16 = 0x0B // LREAL (8 bytes double)
	TypeString uint16 = 0x0C // STRING

	// Pseudo-types for internal use
	TypeUnknown uint16 = 0xFFFF

	// Array flag - high bit indicates array type
	TypeArrayFlag uint16 = 0x8000
)

// IsArray returns true if the type code represents an array.
func IsArray(typeCode uint16) bool {
	return (typeCode & TypeArrayFlag) != 0
}

// MakeArrayType returns the array version of a base type.
func MakeArrayType(baseType uint16) uint16 {
	return baseType | TypeArrayFlag
}

// BaseType returns the base type code with array flag removed.
func BaseType(typeCode uint16) uint16 {
	return typeCode &^ TypeArrayFlag
}

// TypeName returns the human-readable name for a FINS data type.
func TypeName(typeCode uint16) string {
	baseType := BaseType(typeCode)
	isArr := IsArray(typeCode)

	var name string
	switch baseType {
	case TypeVoid:
		name = "VOID"
	case TypeBool:
		name = "BOOL"
	case TypeByte:
		name = "BYTE"
	case TypeSByte:
		name = "SINT"
	case TypeWord:
		name = "WORD"
	case TypeInt16:
		name = "INT"
	case TypeDWord:
		name = "DWORD"
	case TypeInt32:
		name = "DINT"
	case TypeLWord:
		name = "LWORD"
	case TypeInt64:
		name = "LINT"
	case TypeReal:
		name = "REAL"
	case TypeLReal:
		name = "LREAL"
	case TypeString:
		name = "STRING"
	default:
		name = fmt.Sprintf("TYPE_%04X", baseType)
	}

	if isArr {
		return name + "[]"
	}
	return name
}

// TypeCodeFromName returns the type code for a type name.
// Returns TypeUnknown if not recognized.
func TypeCodeFromName(name string) (uint16, bool) {
	switch name {
	case "VOID":
		return TypeVoid, true
	case "BOOL":
		return TypeBool, true
	case "BYTE", "USINT":
		return TypeByte, true
	case "SINT":
		return TypeSByte, true
	case "WORD", "UINT":
		return TypeWord, true
	case "INT":
		return TypeInt16, true
	case "DWORD", "UDINT":
		return TypeDWord, true
	case "DINT":
		return TypeInt32, true
	case "LWORD", "ULINT":
		return TypeLWord, true
	case "LINT":
		return TypeInt64, true
	case "REAL":
		return TypeReal, true
	case "LREAL":
		return TypeLReal, true
	case "STRING":
		return TypeString, true
	default:
		return TypeUnknown, false
	}
}

// TypeSize returns the byte size for a primitive type code.
// Returns 0 for variable-length or unknown types.
func TypeSize(typeCode uint16) int {
	switch BaseType(typeCode) {
	case TypeBool:
		return 2 // BOOL is read as a word in FINS
	case TypeByte, TypeSByte:
		return 1
	case TypeWord, TypeInt16:
		return 2
	case TypeDWord, TypeInt32, TypeReal:
		return 4
	case TypeLWord, TypeInt64, TypeLReal:
		return 8
	default:
		return 0 // Variable or unknown
	}
}

// DecodeValue decodes raw bytes into a Go value based on the type code.
// FINS uses big-endian byte order (native Omron format).
func DecodeValue(typeCode uint16, data []byte) interface{} {
	switch typeCode {
	case TypeBool:
		if len(data) < 2 {
			return false
		}
		// FINS reads bools as words; check if non-zero
		return binary.BigEndian.Uint16(data) != 0

	case TypeByte:
		if len(data) < 1 {
			return uint8(0)
		}
		return data[0]

	case TypeSByte:
		if len(data) < 1 {
			return int8(0)
		}
		return int8(data[0])

	case TypeWord:
		if len(data) < 2 {
			return uint16(0)
		}
		return binary.BigEndian.Uint16(data)

	case TypeInt16:
		if len(data) < 2 {
			return int16(0)
		}
		return int16(binary.BigEndian.Uint16(data))

	case TypeDWord:
		if len(data) < 4 {
			return uint32(0)
		}
		return binary.BigEndian.Uint32(data)

	case TypeInt32:
		if len(data) < 4 {
			return int32(0)
		}
		return int32(binary.BigEndian.Uint32(data))

	case TypeLWord:
		if len(data) < 8 {
			return uint64(0)
		}
		return binary.BigEndian.Uint64(data)

	case TypeInt64:
		if len(data) < 8 {
			return int64(0)
		}
		return int64(binary.BigEndian.Uint64(data))

	case TypeReal:
		if len(data) < 4 {
			return float32(0)
		}
		return math.Float32frombits(binary.BigEndian.Uint32(data))

	case TypeLReal:
		if len(data) < 8 {
			return float64(0)
		}
		return math.Float64frombits(binary.BigEndian.Uint64(data))

	case TypeString:
		// Find null terminator
		for i, b := range data {
			if b == 0 {
				return string(data[:i])
			}
		}
		return string(data)

	default:
		// Return raw bytes for unknown types
		return data
	}
}

// EncodeValue encodes a Go value into bytes for writing to the PLC.
// FINS uses big-endian byte order.
// Returns the encoded bytes, or an error.
func EncodeValue(value interface{}, typeCode uint16) ([]byte, error) {
	switch typeCode {
	case TypeBool:
		var v uint16
		switch b := value.(type) {
		case bool:
			if b {
				v = 1
			}
		case int:
			if b != 0 {
				v = 1
			}
		case int32:
			if b != 0 {
				v = 1
			}
		case int64:
			if b != 0 {
				v = 1
			}
		default:
			return nil, fmt.Errorf("cannot convert %T to BOOL", value)
		}
		buf := make([]byte, 2)
		binary.BigEndian.PutUint16(buf, v)
		return buf, nil

	case TypeByte:
		switch v := value.(type) {
		case uint8:
			return []byte{v}, nil
		case int:
			return []byte{byte(v)}, nil
		case int64:
			return []byte{byte(v)}, nil
		default:
			return nil, fmt.Errorf("cannot convert %T to BYTE", value)
		}

	case TypeSByte:
		switch v := value.(type) {
		case int8:
			return []byte{byte(v)}, nil
		case int:
			return []byte{byte(v)}, nil
		case int64:
			return []byte{byte(v)}, nil
		default:
			return nil, fmt.Errorf("cannot convert %T to SINT", value)
		}

	case TypeWord:
		buf := make([]byte, 2)
		switch v := value.(type) {
		case uint16:
			binary.BigEndian.PutUint16(buf, v)
		case int:
			binary.BigEndian.PutUint16(buf, uint16(v))
		case int64:
			binary.BigEndian.PutUint16(buf, uint16(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to WORD", value)
		}
		return buf, nil

	case TypeInt16:
		buf := make([]byte, 2)
		switch v := value.(type) {
		case int16:
			binary.BigEndian.PutUint16(buf, uint16(v))
		case int:
			binary.BigEndian.PutUint16(buf, uint16(v))
		case int64:
			binary.BigEndian.PutUint16(buf, uint16(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to INT", value)
		}
		return buf, nil

	case TypeDWord:
		buf := make([]byte, 4)
		switch v := value.(type) {
		case uint32:
			binary.BigEndian.PutUint32(buf, v)
		case int:
			binary.BigEndian.PutUint32(buf, uint32(v))
		case int64:
			binary.BigEndian.PutUint32(buf, uint32(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to DWORD", value)
		}
		return buf, nil

	case TypeInt32:
		buf := make([]byte, 4)
		switch v := value.(type) {
		case int32:
			binary.BigEndian.PutUint32(buf, uint32(v))
		case int:
			binary.BigEndian.PutUint32(buf, uint32(v))
		case int64:
			binary.BigEndian.PutUint32(buf, uint32(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to DINT", value)
		}
		return buf, nil

	case TypeLWord:
		buf := make([]byte, 8)
		switch v := value.(type) {
		case uint64:
			binary.BigEndian.PutUint64(buf, v)
		case int:
			binary.BigEndian.PutUint64(buf, uint64(v))
		case int64:
			binary.BigEndian.PutUint64(buf, uint64(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to LWORD", value)
		}
		return buf, nil

	case TypeInt64:
		buf := make([]byte, 8)
		switch v := value.(type) {
		case int64:
			binary.BigEndian.PutUint64(buf, uint64(v))
		case int:
			binary.BigEndian.PutUint64(buf, uint64(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to LINT", value)
		}
		return buf, nil

	case TypeReal:
		buf := make([]byte, 4)
		switch v := value.(type) {
		case float32:
			binary.BigEndian.PutUint32(buf, math.Float32bits(v))
		case float64:
			binary.BigEndian.PutUint32(buf, math.Float32bits(float32(v)))
		case int:
			binary.BigEndian.PutUint32(buf, math.Float32bits(float32(v)))
		default:
			return nil, fmt.Errorf("cannot convert %T to REAL", value)
		}
		return buf, nil

	case TypeLReal:
		buf := make([]byte, 8)
		switch v := value.(type) {
		case float64:
			binary.BigEndian.PutUint64(buf, math.Float64bits(v))
		case float32:
			binary.BigEndian.PutUint64(buf, math.Float64bits(float64(v)))
		case int:
			binary.BigEndian.PutUint64(buf, math.Float64bits(float64(v)))
		default:
			return nil, fmt.Errorf("cannot convert %T to LREAL", value)
		}
		return buf, nil

	case TypeString:
		switch v := value.(type) {
		case string:
			return append([]byte(v), 0), nil
		case []byte:
			return append(v, 0), nil
		default:
			return nil, fmt.Errorf("cannot convert %T to STRING", value)
		}

	default:
		return nil, fmt.Errorf("unsupported type code: %s", TypeName(typeCode))
	}
}

// MemoryAreaName returns the human-readable name for a memory area code.
func MemoryAreaName(area byte) string {
	switch area {
	case MemoryAreaCIOBit, MemoryAreaCIOWord:
		return "CIO"
	case MemoryAreaWRBit, MemoryAreaWRWord:
		return "WR"
	case MemoryAreaHRBit, MemoryAreaHRWord:
		return "HR"
	case MemoryAreaARBit, MemoryAreaARWord:
		return "AR"
	case MemoryAreaDMBit, MemoryAreaDMWord:
		return "DM"
	case MemoryAreaTaskBit:
		return "TK"
	case MemoryAreaTimerCounterPV:
		return "TC"
	default:
		return fmt.Sprintf("AREA_%02X", area)
	}
}

// MemoryAreaFromName returns the memory area code for a name.
// Returns the word area code for word-level access.
func MemoryAreaFromName(name string) (byte, bool) {
	switch name {
	case "CIO", "C":
		return MemoryAreaCIOWord, true
	case "WR", "W":
		return MemoryAreaWRWord, true
	case "HR", "H":
		return MemoryAreaHRWord, true
	case "AR", "A":
		return MemoryAreaARWord, true
	case "DM", "D":
		return MemoryAreaDMWord, true
	case "TK", "T":
		return MemoryAreaTaskBit, true
	case "TC":
		return MemoryAreaTimerCounterPV, true
	default:
		return 0, false
	}
}

// BitAreaFromWordArea converts a word area code to its corresponding bit area code.
func BitAreaFromWordArea(wordArea byte) byte {
	switch wordArea {
	case MemoryAreaCIOWord:
		return MemoryAreaCIOBit
	case MemoryAreaWRWord:
		return MemoryAreaWRBit
	case MemoryAreaHRWord:
		return MemoryAreaHRBit
	case MemoryAreaARWord:
		return MemoryAreaARBit
	case MemoryAreaDMWord:
		return MemoryAreaDMBit
	default:
		return wordArea
	}
}
