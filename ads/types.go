package ads

import (
	"encoding/binary"
	"fmt"
	"math"
)

// ADS/TwinCAT data type codes
// These match the internal TwinCAT type system.
// ADS uses little-endian byte order (native x86 format).
const (
	TypeVoid   uint16 = 0x00
	TypeBool   uint16 = 0x21 // BOOL (1 byte)
	TypeByte   uint16 = 0x11 // BYTE/USINT (1 byte unsigned)
	TypeSByte  uint16 = 0x10 // SINT (1 byte signed)
	TypeWord   uint16 = 0x12 // WORD/UINT (2 bytes unsigned)
	TypeInt16  uint16 = 0x02 // INT (2 bytes signed)
	TypeDWord  uint16 = 0x13 // DWORD/UDINT (4 bytes unsigned)
	TypeInt32  uint16 = 0x03 // DINT (4 bytes signed)
	TypeLWord  uint16 = 0x15 // LWORD/ULINT (8 bytes unsigned)
	TypeInt64  uint16 = 0x14 // LINT (8 bytes signed)
	TypeReal   uint16 = 0x04 // REAL (4 bytes float)
	TypeLReal  uint16 = 0x05 // LREAL (8 bytes double)
	TypeString uint16 = 0x1E // STRING
	TypeWString uint16 = 0x1F // WSTRING
	TypeTime   uint16 = 0x30 // TIME
	TypeDate   uint16 = 0x31 // DATE
	TypeTimeOfDay uint16 = 0x32 // TIME_OF_DAY
	TypeDateTime uint16 = 0x33 // DATE_AND_TIME

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

// TypeName returns the human-readable name for an ADS data type.
func TypeName(typeCode uint16) string {
	switch typeCode {
	case TypeVoid:
		return "VOID"
	case TypeBool:
		return "BOOL"
	case TypeByte:
		return "BYTE"
	case TypeSByte:
		return "SINT"
	case TypeWord:
		return "WORD"
	case TypeInt16:
		return "INT"
	case TypeDWord:
		return "DWORD"
	case TypeInt32:
		return "DINT"
	case TypeLWord:
		return "LWORD"
	case TypeInt64:
		return "LINT"
	case TypeReal:
		return "REAL"
	case TypeLReal:
		return "LREAL"
	case TypeString:
		return "STRING"
	case TypeWString:
		return "WSTRING"
	case TypeTime:
		return "TIME"
	case TypeDate:
		return "DATE"
	case TypeTimeOfDay:
		return "TIME_OF_DAY"
	case TypeDateTime:
		return "DATE_AND_TIME"
	default:
		return fmt.Sprintf("TYPE_%04X", typeCode)
	}
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
	case "WSTRING":
		return TypeWString, true
	case "TIME":
		return TypeTime, true
	case "DATE":
		return TypeDate, true
	case "TIME_OF_DAY", "TOD":
		return TypeTimeOfDay, true
	case "DATE_AND_TIME", "DT":
		return TypeDateTime, true
	default:
		return TypeUnknown, false
	}
}

// TypeSize returns the byte size for a primitive type code.
// Returns 0 for variable-length or unknown types.
func TypeSize(typeCode uint16) int {
	switch typeCode {
	case TypeBool, TypeByte, TypeSByte:
		return 1
	case TypeWord, TypeInt16:
		return 2
	case TypeDWord, TypeInt32, TypeReal, TypeTime, TypeDate, TypeTimeOfDay:
		return 4
	case TypeLWord, TypeInt64, TypeLReal, TypeDateTime:
		return 8
	default:
		return 0 // Variable or unknown
	}
}

// DecodeValue decodes raw bytes into a Go value based on the type code.
// ADS uses little-endian byte order.
func DecodeValue(typeCode uint16, data []byte) interface{} {
	switch typeCode {
	case TypeBool:
		if len(data) < 1 {
			return false
		}
		return data[0] != 0

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
		return binary.LittleEndian.Uint16(data)

	case TypeInt16:
		if len(data) < 2 {
			return int16(0)
		}
		return int16(binary.LittleEndian.Uint16(data))

	case TypeDWord, TypeTime, TypeDate, TypeTimeOfDay:
		if len(data) < 4 {
			return uint32(0)
		}
		return binary.LittleEndian.Uint32(data)

	case TypeInt32:
		if len(data) < 4 {
			return int32(0)
		}
		return int32(binary.LittleEndian.Uint32(data))

	case TypeLWord:
		if len(data) < 8 {
			return uint64(0)
		}
		return binary.LittleEndian.Uint64(data)

	case TypeInt64, TypeDateTime:
		if len(data) < 8 {
			return int64(0)
		}
		return int64(binary.LittleEndian.Uint64(data))

	case TypeReal:
		if len(data) < 4 {
			return float32(0)
		}
		return math.Float32frombits(binary.LittleEndian.Uint32(data))

	case TypeLReal:
		if len(data) < 8 {
			return float64(0)
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(data))

	case TypeString:
		// TwinCAT strings: 1 byte length + chars (or null-terminated)
		// Find null terminator
		for i, b := range data {
			if b == 0 {
				return string(data[:i])
			}
		}
		return string(data)

	case TypeWString:
		// Wide strings: 2 bytes per char, null-terminated
		var chars []rune
		for i := 0; i+1 < len(data); i += 2 {
			c := binary.LittleEndian.Uint16(data[i:])
			if c == 0 {
				break
			}
			chars = append(chars, rune(c))
		}
		return string(chars)

	default:
		// Return raw bytes for unknown types
		return data
	}
}

// EncodeValue encodes a Go value into bytes for writing to the PLC.
// Returns the encoded bytes and the ADS type code, or an error.
func EncodeValue(value interface{}) ([]byte, uint16, error) {
	switch v := value.(type) {
	case bool:
		if v {
			return []byte{1}, TypeBool, nil
		}
		return []byte{0}, TypeBool, nil

	case int8:
		return []byte{byte(v)}, TypeSByte, nil

	case uint8:
		return []byte{v}, TypeByte, nil

	case int16:
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, uint16(v))
		return buf, TypeInt16, nil

	case uint16:
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, v)
		return buf, TypeWord, nil

	case int32:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v))
		return buf, TypeInt32, nil

	case uint32:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, v)
		return buf, TypeDWord, nil

	case int64:
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(v))
		return buf, TypeInt64, nil

	case uint64:
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, v)
		return buf, TypeLWord, nil

	case int:
		// Default int to DINT (most common)
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v))
		return buf, TypeInt32, nil

	case uint:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, uint32(v))
		return buf, TypeDWord, nil

	case float32:
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, math.Float32bits(v))
		return buf, TypeReal, nil

	case float64:
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, math.Float64bits(v))
		return buf, TypeLReal, nil

	case string:
		// TwinCAT strings: just the bytes + null terminator
		buf := append([]byte(v), 0)
		return buf, TypeString, nil

	case []byte:
		// Raw bytes - caller must know the type
		return v, TypeUnknown, nil

	default:
		return nil, TypeUnknown, fmt.Errorf("unsupported value type: %T", value)
	}
}

// EncodeValueWithType encodes a value using a specific type code.
// This is used when we know the target type from symbol info.
func EncodeValueWithType(value interface{}, typeCode uint16) ([]byte, error) {
	size := TypeSize(typeCode)
	if size == 0 && typeCode != TypeString && typeCode != TypeWString {
		return nil, fmt.Errorf("cannot encode to type %s", TypeName(typeCode))
	}

	switch typeCode {
	case TypeBool:
		var b byte
		switch v := value.(type) {
		case bool:
			if v {
				b = 1
			}
		case int:
			if v != 0 {
				b = 1
			}
		case int32:
			if v != 0 {
				b = 1
			}
		case int64:
			if v != 0 {
				b = 1
			}
		default:
			return nil, fmt.Errorf("cannot convert %T to BOOL", value)
		}
		return []byte{b}, nil

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
			binary.LittleEndian.PutUint16(buf, v)
		case int:
			binary.LittleEndian.PutUint16(buf, uint16(v))
		case int64:
			binary.LittleEndian.PutUint16(buf, uint16(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to WORD", value)
		}
		return buf, nil

	case TypeInt16:
		buf := make([]byte, 2)
		switch v := value.(type) {
		case int16:
			binary.LittleEndian.PutUint16(buf, uint16(v))
		case int:
			binary.LittleEndian.PutUint16(buf, uint16(v))
		case int64:
			binary.LittleEndian.PutUint16(buf, uint16(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to INT", value)
		}
		return buf, nil

	case TypeDWord:
		buf := make([]byte, 4)
		switch v := value.(type) {
		case uint32:
			binary.LittleEndian.PutUint32(buf, v)
		case int:
			binary.LittleEndian.PutUint32(buf, uint32(v))
		case int64:
			binary.LittleEndian.PutUint32(buf, uint32(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to DWORD", value)
		}
		return buf, nil

	case TypeInt32:
		buf := make([]byte, 4)
		switch v := value.(type) {
		case int32:
			binary.LittleEndian.PutUint32(buf, uint32(v))
		case int:
			binary.LittleEndian.PutUint32(buf, uint32(v))
		case int64:
			binary.LittleEndian.PutUint32(buf, uint32(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to DINT", value)
		}
		return buf, nil

	case TypeLWord:
		buf := make([]byte, 8)
		switch v := value.(type) {
		case uint64:
			binary.LittleEndian.PutUint64(buf, v)
		case int:
			binary.LittleEndian.PutUint64(buf, uint64(v))
		case int64:
			binary.LittleEndian.PutUint64(buf, uint64(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to LWORD", value)
		}
		return buf, nil

	case TypeInt64:
		buf := make([]byte, 8)
		switch v := value.(type) {
		case int64:
			binary.LittleEndian.PutUint64(buf, uint64(v))
		case int:
			binary.LittleEndian.PutUint64(buf, uint64(v))
		default:
			return nil, fmt.Errorf("cannot convert %T to LINT", value)
		}
		return buf, nil

	case TypeReal:
		buf := make([]byte, 4)
		switch v := value.(type) {
		case float32:
			binary.LittleEndian.PutUint32(buf, math.Float32bits(v))
		case float64:
			binary.LittleEndian.PutUint32(buf, math.Float32bits(float32(v)))
		case int:
			binary.LittleEndian.PutUint32(buf, math.Float32bits(float32(v)))
		default:
			return nil, fmt.Errorf("cannot convert %T to REAL", value)
		}
		return buf, nil

	case TypeLReal:
		buf := make([]byte, 8)
		switch v := value.(type) {
		case float64:
			binary.LittleEndian.PutUint64(buf, math.Float64bits(v))
		case float32:
			binary.LittleEndian.PutUint64(buf, math.Float64bits(float64(v)))
		case int:
			binary.LittleEndian.PutUint64(buf, math.Float64bits(float64(v)))
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
