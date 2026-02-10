package valkey

import (
	"encoding/json"
	"testing"
	"time"
)

// TestTagMessage_Structure tests the TagMessage JSON structure.
func TestTagMessage_Structure(t *testing.T) {
	t.Run("all fields present", func(t *testing.T) {
		msg := TagMessage{
			Factory:   "warlink",
			PLC:       "plc1",
			Tag:       "Counter",
			Value:     int32(100),
			Type:      "DINT",
			Writable:  true,
			Timestamp: time.Now().UTC(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// Verify required fields
		requiredFields := []string{"factory", "plc", "tag", "value", "type", "writable", "timestamp"}
		for _, field := range requiredFields {
			if _, ok := decoded[field]; !ok {
				t.Errorf("missing required field: %s", field)
			}
		}
	})

	t.Run("alias message includes memloc", func(t *testing.T) {
		msg := TagMessage{
			Factory:   "warlink",
			PLC:       "s7",
			Tag:       "sensor_temp",  // alias
			MemLoc:    "DB1.0",        // original address
			Value:     int32(25),
			Type:      "DINT",
			Writable:  false,
			Timestamp: time.Now().UTC(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// Verify alias is in tag field
		if decoded["tag"] != "sensor_temp" {
			t.Errorf("expected tag 'sensor_temp', got %v", decoded["tag"])
		}

		// Verify original address is in memloc field
		if decoded["memloc"] != "DB1.0" {
			t.Errorf("expected memloc 'DB1.0', got %v", decoded["memloc"])
		}
	})

	t.Run("non-alias message omits memloc", func(t *testing.T) {
		msg := TagMessage{
			Factory:   "warlink",
			PLC:       "logix",
			Tag:       "Counter",
			MemLoc:    "",  // empty
			Value:     int32(100),
			Type:      "DINT",
			Writable:  true,
			Timestamp: time.Now().UTC(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// memloc should be omitted when empty
		if _, ok := decoded["memloc"]; ok {
			t.Error("memloc should be omitted when empty")
		}
	})
}

// TestTagMessage_ValueAccuracy tests that published values match source values.
func TestTagMessage_ValueAccuracy(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		value    interface{}
	}{
		{"int32_max", "DINT", int32(2147483647)},
		{"int32_min", "DINT", int32(-2147483648)},
		{"int32_zero", "DINT", int32(0)},
		{"int16_max", "INT", int16(32767)},
		{"int16_min", "INT", int16(-32768)},
		{"uint16_max", "UINT", uint16(65535)},
		{"uint8_max", "USINT", uint8(255)},
		{"float32_precise", "REAL", float32(3.14159)},
		{"float64_precise", "LREAL", float64(3.141592653589793)},
		{"bool_true", "BOOL", true},
		{"bool_false", "BOOL", false},
		{"string_ascii", "STRING", "Hello, World!"},
		{"string_unicode", "STRING", "测试数据"},
		{"string_special", "STRING", "Line1\nLine2\tTab"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := TagMessage{
				Factory:   "warlink",
				PLC:       "test",
				Tag:       "tag",
				Value:     tc.value,
				Type:      tc.typeName,
				Timestamp: time.Now().UTC(),
			}

			data, err := json.Marshal(msg)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}

			var decoded TagMessage
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}

			// Check value accuracy (JSON numbers become float64)
			switch v := tc.value.(type) {
			case int32:
				if decoded.Value.(float64) != float64(v) {
					t.Errorf("int32 value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case int16:
				if decoded.Value.(float64) != float64(v) {
					t.Errorf("int16 value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case uint16:
				if decoded.Value.(float64) != float64(v) {
					t.Errorf("uint16 value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case uint8:
				if decoded.Value.(float64) != float64(v) {
					t.Errorf("uint8 value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case float32:
				// Float32 loses precision
				if diff := decoded.Value.(float64) - float64(v); diff > 0.0001 || diff < -0.0001 {
					t.Errorf("float32 value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case float64:
				if decoded.Value.(float64) != v {
					t.Errorf("float64 value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case bool:
				if decoded.Value.(bool) != v {
					t.Errorf("bool value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case string:
				if decoded.Value.(string) != v {
					t.Errorf("string value mismatch: expected %q, got %q", v, decoded.Value)
				}
			}
		})
	}
}

// TestTagPublishItem_Structure tests the batch publish item structure.
func TestTagPublishItem_Structure(t *testing.T) {
	item := TagPublishItem{
		PLCName:  "s7",
		TagName:  "DB1.0",
		Alias:    "sensor_temp",
		Address:  "DB1.0",
		TypeName: "DINT",
		Value:    int32(25),
		Writable: false,
	}

	// Verify all fields are set
	if item.PLCName != "s7" {
		t.Error("PLCName not set correctly")
	}
	if item.TagName != "DB1.0" {
		t.Error("TagName not set correctly")
	}
	if item.Alias != "sensor_temp" {
		t.Error("Alias not set correctly")
	}
	if item.Address != "DB1.0" {
		t.Error("Address not set correctly")
	}
	if item.TypeName != "DINT" {
		t.Error("TypeName not set correctly")
	}
	if item.Value != int32(25) {
		t.Error("Value not set correctly")
	}
	if item.Writable != false {
		t.Error("Writable not set correctly")
	}
}

// TestWriteRequest_Structure tests the write request JSON structure.
func TestWriteRequest_Structure(t *testing.T) {
	req := WriteRequest{
		Factory: "warlink",
		PLC:     "logix",
		Tag:     "Counter",
		Value:   int32(100),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded WriteRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded.Factory != "warlink" {
		t.Errorf("Factory mismatch: expected 'warlink', got %q", decoded.Factory)
	}
	if decoded.PLC != "logix" {
		t.Errorf("PLC mismatch: expected 'logix', got %q", decoded.PLC)
	}
	if decoded.Tag != "Counter" {
		t.Errorf("Tag mismatch: expected 'Counter', got %q", decoded.Tag)
	}
}

// TestWriteResponse_Structure tests the write response JSON structure.
func TestWriteResponse_Structure(t *testing.T) {
	t.Run("successful response", func(t *testing.T) {
		resp := WriteResponse{
			Factory:   "warlink",
			PLC:       "logix",
			Tag:       "Counter",
			Value:     int32(100),
			Success:   true,
			Timestamp: time.Now().UTC(),
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// Success response should not have error field
		if _, ok := decoded["error"]; ok {
			t.Error("successful response should not have error field")
		}

		if decoded["success"] != true {
			t.Error("success should be true")
		}
	})

	t.Run("failed response", func(t *testing.T) {
		resp := WriteResponse{
			Factory:   "warlink",
			PLC:       "logix",
			Tag:       "Counter",
			Value:     int32(100),
			Success:   false,
			Error:     "tag not writable",
			Timestamp: time.Now().UTC(),
		}

		data, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if decoded["success"] != false {
			t.Error("success should be false")
		}

		if decoded["error"] != "tag not writable" {
			t.Errorf("error message mismatch: expected 'tag not writable', got %v", decoded["error"])
		}
	})
}

// TestHealthMessage_Structure tests the health message JSON structure.
func TestHealthMessage_Structure(t *testing.T) {
	t.Run("healthy PLC", func(t *testing.T) {
		msg := HealthMessage{
			Factory:   "warlink",
			PLC:       "logix",
			Driver:    "logix",
			Online:    true,
			Status:    "Connected",
			Timestamp: time.Now().UTC(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// Healthy PLC should not have error field
		if _, ok := decoded["error"]; ok {
			t.Error("healthy PLC should not have error field")
		}

		if decoded["online"] != true {
			t.Error("online should be true")
		}
	})

	t.Run("unhealthy PLC", func(t *testing.T) {
		msg := HealthMessage{
			Factory:   "warlink",
			PLC:       "logix",
			Driver:    "logix",
			Online:    false,
			Status:    "Disconnected",
			Error:     "connection refused",
			Timestamp: time.Now().UTC(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if decoded["online"] != false {
			t.Error("online should be false")
		}

		if decoded["error"] != "connection refused" {
			t.Errorf("error mismatch: expected 'connection refused', got %v", decoded["error"])
		}
	})
}

// TestAliasKeyGeneration tests that keys are generated correctly with aliases.
func TestAliasKeyGeneration(t *testing.T) {
	// This test validates the logic that should be used when generating keys
	// The actual key generation happens in the Publish method using the namespace builder

	tests := []struct {
		name        string
		plcName     string
		tagName     string
		alias       string
		expectInKey string
	}{
		{"with alias", "s7", "DB1.0", "sensor_temp", "sensor_temp"},
		{"without alias", "logix", "Counter", "", "Counter"},
		{"omron with alias", "omron", "D100", "motor_speed", "motor_speed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// The display tag should be used in keys
			displayTag := tc.tagName
			if tc.alias != "" {
				displayTag = tc.alias
			}

			if displayTag != tc.expectInKey {
				t.Errorf("expected %q in key, got %q", tc.expectInKey, displayTag)
			}
		})
	}
}

// TestTimestampFormat tests that timestamps are in the correct format.
func TestTimestampFormat(t *testing.T) {
	msg := TagMessage{
		Factory:   "warlink",
		PLC:       "test",
		Tag:       "tag",
		Value:     int32(100),
		Type:      "DINT",
		Timestamp: time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	// Timestamp should be in RFC3339 format
	ts := decoded["timestamp"].(string)
	if ts != "2024-01-15T10:30:45Z" {
		t.Errorf("unexpected timestamp format: %s", ts)
	}
}

// TestNullValueHandling tests handling of nil values.
func TestNullValueHandling(t *testing.T) {
	msg := TagMessage{
		Factory:   "warlink",
		PLC:       "test",
		Tag:       "tag",
		Value:     nil,
		Type:      "DINT",
		Timestamp: time.Now().UTC(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if decoded["value"] != nil {
		t.Errorf("expected null value, got %v", decoded["value"])
	}
}
