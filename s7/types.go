// Package s7 provides Siemens S7 PLC communication using the S7 protocol.
package s7

import "strings"

// S7 data type codes.
// These are used to interpret raw bytes from the PLC.
// Note: USINT/UINT/UDINT are aliases for BYTE/WORD/DWORD respectively.
const (
	TypeBool   uint16 = 0x0001 // 1 bit
	TypeByte   uint16 = 0x0002 // 8 bits unsigned (also USINT)
	TypeSInt   uint16 = 0x0003 // 8 bits signed
	TypeWord   uint16 = 0x0004 // 16 bits unsigned (also UINT)
	TypeInt    uint16 = 0x0005 // 16 bits signed
	TypeDWord  uint16 = 0x0006 // 32 bits unsigned (also UDINT)
	TypeDInt   uint16 = 0x0007 // 32 bits signed
	TypeReal   uint16 = 0x0008 // 32 bits IEEE 754 float
	TypeLInt   uint16 = 0x000F // 64 bits signed (S7-1500)
	TypeULInt  uint16 = 0x0010 // 64 bits unsigned (S7-1500)
	TypeLReal   uint16 = 0x001E // 64 bits IEEE 754 double (S7-1500)
	TypeString  uint16 = 0x0013 // S7 String (max 254 chars)
	TypeWString uint16 = 0x0014 // Wide string (S7-1500)
)

// TypeSize returns the byte size of a data type.
// Returns 0 for unknown types.
func TypeSize(dataType uint16) int {
	switch dataType {
	case TypeBool:
		return 1 // Stored as 1 byte
	case TypeByte, TypeSInt:
		return 1
	case TypeWord, TypeInt:
		return 2
	case TypeDWord, TypeDInt, TypeReal:
		return 4
	case TypeLInt, TypeULInt, TypeLReal:
		return 8
	default:
		return 0
	}
}

// TypeName returns a human-readable name for the data type.
func TypeName(dataType uint16) string {
	switch dataType {
	case TypeBool:
		return "BOOL"
	case TypeByte:
		return "BYTE"
	case TypeWord:
		return "WORD"
	case TypeDWord:
		return "DWORD"
	case TypeInt:
		return "INT"
	case TypeDInt:
		return "DINT"
	case TypeReal:
		return "REAL"
	case TypeLReal:
		return "LREAL"
	case TypeSInt:
		return "SINT"
	case TypeLInt:
		return "LINT"
	case TypeULInt:
		return "ULINT"
	case TypeString:
		return "STRING"
	case TypeWString:
		return "WSTRING"
	default:
		return "UNKNOWN"
	}
}

// TypeCodeFromName returns the type code for a given type name.
// Returns (typeCode, true) if found, (0, false) otherwise.
func TypeCodeFromName(name string) (uint16, bool) {
	switch strings.ToUpper(name) {
	case "BOOL":
		return TypeBool, true
	case "BYTE", "USINT":
		return TypeByte, true
	case "WORD", "UINT":
		return TypeWord, true
	case "DWORD", "UDINT":
		return TypeDWord, true
	case "SINT":
		return TypeSInt, true
	case "INT":
		return TypeInt, true
	case "DINT":
		return TypeDInt, true
	case "LINT":
		return TypeLInt, true
	case "ULINT":
		return TypeULInt, true
	case "REAL":
		return TypeReal, true
	case "LREAL":
		return TypeLReal, true
	case "STRING":
		return TypeString, true
	case "WSTRING":
		return TypeWString, true
	default:
		return 0, false
	}
}

// SupportedTypeNames returns a list of supported type names for manual tag entry.
func SupportedTypeNames() []string {
	return []string{"BOOL", "BYTE", "SINT", "INT", "DINT", "LINT", "WORD", "DWORD", "REAL", "LREAL", "STRING"}
}
