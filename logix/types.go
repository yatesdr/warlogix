package logix

// Logix CIP data type codes.
// These are returned in the DataType field of Tag after a ReadTag operation.
// The caller uses these to interpret the raw bytes.
const (
	TypeBOOL  uint16 = 0x00C1 // 1 byte (0 or 1)
	TypeSINT  uint16 = 0x00C2 // 1 byte signed
	TypeINT   uint16 = 0x00C3 // 2 bytes signed
	TypeDINT  uint16 = 0x00C4 // 4 bytes signed
	TypeLINT  uint16 = 0x00C5 // 8 bytes signed
	TypeUSINT uint16 = 0x00C6 // 1 byte unsigned
	TypeUINT  uint16 = 0x00C7 // 2 bytes unsigned
	TypeUDINT uint16 = 0x00C8 // 4 bytes unsigned
	TypeULINT uint16 = 0x00C9 // 8 bytes unsigned
	TypeREAL  uint16 = 0x00CA // 4 bytes IEEE 754 float
	TypeLREAL uint16 = 0x00CB // 8 bytes IEEE 754 double

	// String types
	TypeSTRING      uint16 = 0x00D0 // Logix STRING (82 bytes: 4-byte len + 82 chars)
	TypeShortSTRING uint16 = 0x00DA // Short string

	// Bit string types (arrays of bits)
	TypeBitString8  uint16 = 0x00D1 // 8 bits
	TypeBitString16 uint16 = 0x00D2 // 16 bits
	TypeBitString32 uint16 = 0x00D3 // 32 bits

	// Structure flag - when bit 15 is set, this is a structure/UDT.
	// The lower bits contain the template instance ID.
	TypeStructureMask uint16 = 0x8000

	// Array flag - when bit 13 is set, this is an array.
	// Combined with other type info.
	TypeArrayMask uint16 = 0x2000

	// System structure flag
	TypeSystemMask uint16 = 0x1000
)

// TypeSize returns the byte size of atomic types.
// Returns 0 for structures, arrays, or unknown types.
func TypeSize(dataType uint16) int {
	// Mask off array/structure flags for base type
	baseType := dataType & 0x0FFF

	switch baseType {
	case TypeBOOL, TypeSINT, TypeUSINT:
		return 1
	case TypeINT, TypeUINT:
		return 2
	case TypeDINT, TypeUDINT, TypeREAL:
		return 4
	case TypeLINT, TypeULINT, TypeLREAL:
		return 8
	default:
		return 0
	}
}

// IsStructure returns true if the data type represents a structure/UDT.
func IsStructure(dataType uint16) bool {
	return (dataType & TypeStructureMask) != 0
}

// IsArray returns true if the data type represents an array.
func IsArray(dataType uint16) bool {
	return (dataType & TypeArrayMask) != 0
}

// TypeName returns a human-readable name for the data type.
func TypeName(dataType uint16) string {
	if IsStructure(dataType) {
		return "STRUCT"
	}

	baseType := dataType & 0x0FFF
	var name string

	switch baseType {
	case TypeBOOL:
		name = "BOOL"
	case TypeSINT:
		name = "SINT"
	case TypeINT:
		name = "INT"
	case TypeDINT:
		name = "DINT"
	case TypeLINT:
		name = "LINT"
	case TypeUSINT:
		name = "USINT"
	case TypeUINT:
		name = "UINT"
	case TypeUDINT:
		name = "UDINT"
	case TypeULINT:
		name = "ULINT"
	case TypeREAL:
		name = "REAL"
	case TypeLREAL:
		name = "LREAL"
	case TypeSTRING:
		name = "STRING"
	case TypeShortSTRING:
		name = "SHORT_STRING"
	default:
		name = "UNKNOWN"
	}

	if IsArray(dataType) {
		return name + "[]"
	}
	return name
}
