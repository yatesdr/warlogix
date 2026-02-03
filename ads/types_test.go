package ads

import (
	"testing"
)

func TestParseAmsNetId(t *testing.T) {
	tests := []struct {
		input   string
		want    AmsNetId
		wantErr bool
	}{
		{"192.168.1.100.1.1", AmsNetId{192, 168, 1, 100, 1, 1}, false},
		{"10.0.0.1.1.1", AmsNetId{10, 0, 0, 1, 1, 1}, false},
		{"0.0.0.0.0.0", AmsNetId{0, 0, 0, 0, 0, 0}, false},
		{"255.255.255.255.255.255", AmsNetId{255, 255, 255, 255, 255, 255}, false},
		{"192.168.1.100", AmsNetId{}, true},      // Too few parts
		{"192.168.1.100.1.1.1", AmsNetId{}, true}, // Too many parts
		{"", AmsNetId{}, true},                    // Empty
		{"abc.def.ghi.jkl.mno.pqr", AmsNetId{}, true}, // Non-numeric
		{"256.0.0.0.0.0", AmsNetId{}, true},       // Out of range
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseAmsNetId(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAmsNetId(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseAmsNetId(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAmsNetIdFromIP(t *testing.T) {
	tests := []struct {
		input   string
		want    AmsNetId
		wantErr bool
	}{
		{"192.168.1.100", AmsNetId{192, 168, 1, 100, 1, 1}, false},
		{"192.168.1.100:48898", AmsNetId{192, 168, 1, 100, 1, 1}, false}, // With port
		{"10.0.0.1", AmsNetId{10, 0, 0, 1, 1, 1}, false},
		{"invalid", AmsNetId{}, true},
		{"", AmsNetId{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := AmsNetIdFromIP(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("AmsNetIdFromIP(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("AmsNetIdFromIP(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestAmsNetIdString(t *testing.T) {
	netId := AmsNetId{192, 168, 1, 100, 1, 1}
	want := "192.168.1.100.1.1"
	got := netId.String()
	if got != want {
		t.Errorf("AmsNetId.String() = %q, want %q", got, want)
	}
}

func TestTypeCodeFromName(t *testing.T) {
	tests := []struct {
		name     string
		wantCode uint16
		wantOk   bool
	}{
		{"BOOL", TypeBool, true},
		{"BYTE", TypeByte, true},
		{"USINT", TypeByte, true},
		{"SINT", TypeSByte, true},
		{"WORD", TypeWord, true},
		{"UINT", TypeWord, true},
		{"INT", TypeInt16, true},
		{"DWORD", TypeDWord, true},
		{"UDINT", TypeDWord, true},
		{"DINT", TypeInt32, true},
		{"LWORD", TypeLWord, true},
		{"ULINT", TypeLWord, true},
		{"LINT", TypeInt64, true},
		{"REAL", TypeReal, true},
		{"LREAL", TypeLReal, true},
		{"STRING", TypeString, true},
		{"UNKNOWN", TypeUnknown, false},
		{"", TypeUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCode, gotOk := TypeCodeFromName(tt.name)
			if gotCode != tt.wantCode || gotOk != tt.wantOk {
				t.Errorf("TypeCodeFromName(%q) = (%d, %v), want (%d, %v)",
					tt.name, gotCode, gotOk, tt.wantCode, tt.wantOk)
			}
		})
	}
}

func TestTypeName(t *testing.T) {
	tests := []struct {
		code uint16
		want string
	}{
		{TypeBool, "BOOL"},
		{TypeByte, "BYTE"},
		{TypeSByte, "SINT"},
		{TypeWord, "WORD"},
		{TypeInt16, "INT"},
		{TypeDWord, "DWORD"},
		{TypeInt32, "DINT"},
		{TypeLWord, "LWORD"},
		{TypeInt64, "LINT"},
		{TypeReal, "REAL"},
		{TypeLReal, "LREAL"},
		{TypeString, "STRING"},
		{0x99, "TYPE_0099"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := TypeName(tt.code)
			if got != tt.want {
				t.Errorf("TypeName(%d) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

func TestDecodeValue(t *testing.T) {
	tests := []struct {
		name     string
		typeCode uint16
		data     []byte
		want     interface{}
	}{
		{"BOOL true", TypeBool, []byte{1}, true},
		{"BOOL false", TypeBool, []byte{0}, false},
		{"BYTE", TypeByte, []byte{42}, uint8(42)},
		{"SINT positive", TypeSByte, []byte{42}, int8(42)},
		{"SINT negative", TypeSByte, []byte{0xFE}, int8(-2)},
		{"WORD", TypeWord, []byte{0x34, 0x12}, uint16(0x1234)},
		{"INT positive", TypeInt16, []byte{0x34, 0x12}, int16(0x1234)},
		{"INT negative", TypeInt16, []byte{0xFE, 0xFF}, int16(-2)},
		{"DWORD", TypeDWord, []byte{0x78, 0x56, 0x34, 0x12}, uint32(0x12345678)},
		{"DINT positive", TypeInt32, []byte{0x78, 0x56, 0x34, 0x12}, int32(0x12345678)},
		{"DINT negative", TypeInt32, []byte{0xFE, 0xFF, 0xFF, 0xFF}, int32(-2)},
		{"REAL", TypeReal, []byte{0x00, 0x00, 0x48, 0x42}, float32(50.0)},
		{"STRING", TypeString, []byte{'H', 'e', 'l', 'l', 'o', 0}, "Hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeValue(tt.typeCode, tt.data)
			// Use type assertion for comparison
			switch want := tt.want.(type) {
			case float32:
				if gotFloat, ok := got.(float32); !ok || gotFloat != want {
					t.Errorf("DecodeValue() = %v (%T), want %v (%T)", got, got, want, want)
				}
			default:
				if got != want {
					t.Errorf("DecodeValue() = %v (%T), want %v (%T)", got, got, want, want)
				}
			}
		})
	}
}

func TestEncodeValue(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		wantData []byte
		wantType uint16
		wantErr  bool
	}{
		{"bool true", true, []byte{1}, TypeBool, false},
		{"bool false", false, []byte{0}, TypeBool, false},
		{"int8", int8(42), []byte{42}, TypeSByte, false},
		{"uint8", uint8(42), []byte{42}, TypeByte, false},
		{"int16", int16(0x1234), []byte{0x34, 0x12}, TypeInt16, false},
		{"uint16", uint16(0x1234), []byte{0x34, 0x12}, TypeWord, false},
		{"int32", int32(0x12345678), []byte{0x78, 0x56, 0x34, 0x12}, TypeInt32, false},
		{"uint32", uint32(0x12345678), []byte{0x78, 0x56, 0x34, 0x12}, TypeDWord, false},
		{"int", int(100), []byte{100, 0, 0, 0}, TypeInt32, false},
		{"float32", float32(50.0), []byte{0x00, 0x00, 0x48, 0x42}, TypeReal, false},
		{"string", "Hi", []byte{'H', 'i', 0}, TypeString, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotData, gotType, err := EncodeValue(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("EncodeValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if gotType != tt.wantType {
					t.Errorf("EncodeValue() type = %d, want %d", gotType, tt.wantType)
				}
				if len(gotData) != len(tt.wantData) {
					t.Errorf("EncodeValue() data len = %d, want %d", len(gotData), len(tt.wantData))
				} else {
					for i := range gotData {
						if gotData[i] != tt.wantData[i] {
							t.Errorf("EncodeValue() data[%d] = %d, want %d", i, gotData[i], tt.wantData[i])
						}
					}
				}
			}
		})
	}
}
