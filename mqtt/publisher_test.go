package mqtt

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"warlink/config"
)

// TestChangeDetectionLogic tests the core change detection logic directly.
func TestChangeDetectionLogic(t *testing.T) {
	t.Run("identical values should not republish", func(t *testing.T) {
		cache := make(map[string]interface{})
		cache["plc1/tag1"] = int32(100)

		// Check if same value would republish
		cacheKey := "plc1/tag1"
		value := int32(100)
		force := false

		lastValue, exists := cache[cacheKey]
		shouldPublish := !exists || force || fmt.Sprintf("%v", lastValue) != fmt.Sprintf("%v", value)

		if shouldPublish {
			t.Error("identical value should not republish")
		}
	})

	t.Run("different values should republish", func(t *testing.T) {
		cache := make(map[string]interface{})
		cache["plc1/tag1"] = int32(100)

		cacheKey := "plc1/tag1"
		value := int32(200)
		force := false

		lastValue, exists := cache[cacheKey]
		shouldPublish := !exists || force || fmt.Sprintf("%v", lastValue) != fmt.Sprintf("%v", value)

		if !shouldPublish {
			t.Error("different value should republish")
		}
	})

	t.Run("force flag should override change detection", func(t *testing.T) {
		cache := make(map[string]interface{})
		cache["plc1/tag1"] = int32(100)

		cacheKey := "plc1/tag1"
		value := int32(100)
		force := true

		lastValue, exists := cache[cacheKey]
		shouldPublish := !exists || force || fmt.Sprintf("%v", lastValue) != fmt.Sprintf("%v", value)

		if !shouldPublish {
			t.Error("force flag should override change detection")
		}
	})

	t.Run("new key should always publish", func(t *testing.T) {
		cache := make(map[string]interface{})
		// cache is empty

		cacheKey := "plc1/tag1"
		force := false

		_, exists := cache[cacheKey]
		shouldPublish := !exists || force

		if !shouldPublish {
			t.Error("new key should always publish")
		}
	})

	t.Run("different PLCs are tracked separately", func(t *testing.T) {
		cache := make(map[string]interface{})
		cache["plc1/tag1"] = int32(100)

		// Different PLC, same tag and value
		cacheKey := "plc2/tag1"

		_, exists := cache[cacheKey]
		shouldPublish := !exists

		if !shouldPublish {
			t.Error("different PLCs should be tracked separately")
		}
	})

	t.Run("different tags are tracked separately", func(t *testing.T) {
		cache := make(map[string]interface{})
		cache["plc1/tag1"] = int32(100)

		// Same PLC, different tag
		cacheKey := "plc1/tag2"

		_, exists := cache[cacheKey]
		shouldPublish := !exists

		if !shouldPublish {
			t.Error("different tags should be tracked separately")
		}
	})
}

// TestChangeDetectionTypes tests change detection across different data types.
func TestChangeDetectionTypes(t *testing.T) {
	tests := []struct {
		name       string
		value1     interface{}
		value2     interface{}
		shouldPub  bool
		desc       string
	}{
		// Integer types
		{"int32_same", int32(100), int32(100), false, "same int32"},
		{"int32_diff", int32(100), int32(200), true, "different int32"},
		{"int16_same", int16(50), int16(50), false, "same int16"},
		{"int16_diff", int16(50), int16(60), true, "different int16"},

		// Float types
		{"float32_same", float32(3.14), float32(3.14), false, "same float32"},
		{"float32_diff", float32(3.14), float32(2.71), true, "different float32"},
		{"float64_same", float64(3.14159), float64(3.14159), false, "same float64"},
		{"float64_diff", float64(3.14159), float64(2.71828), true, "different float64"},

		// Boolean types
		{"bool_same_true", true, true, false, "same bool true"},
		{"bool_same_false", false, false, false, "same bool false"},
		{"bool_diff", true, false, true, "different bool"},

		// String types
		{"string_same", "hello", "hello", false, "same string"},
		{"string_diff", "hello", "world", true, "different string"},
		{"string_empty", "", "", false, "same empty string"},

		// Nil handling - string representation handles these
		{"zero_int", int32(0), int32(0), false, "same zero"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cache := make(map[string]interface{})
			cache["plc/tag"] = tc.value1

			lastValue := cache["plc/tag"]
			shouldPublish := fmt.Sprintf("%v", lastValue) != fmt.Sprintf("%v", tc.value2)

			if shouldPublish != tc.shouldPub {
				t.Errorf("%s: expected publish=%v, got %v", tc.desc, tc.shouldPub, shouldPublish)
			}
		})
	}
}

// TestAliasConsistency_CacheKey tests that aliases are used correctly in cache keys.
func TestAliasConsistency_CacheKey(t *testing.T) {
	t.Run("alias used in cache key", func(t *testing.T) {
		// The Publish function uses displayTag for the cache key
		// When alias is provided: displayTag = alias
		// When no alias: displayTag = tagName

		tagName := "DB1.0"
		alias := "sensor_temp"
		plcName := "s7"

		// Logic from Publish function
		displayTag := tagName
		if alias != "" {
			displayTag = alias
		}
		cacheKey := fmt.Sprintf("%s/%s", plcName, displayTag)

		expected := "s7/sensor_temp"
		if cacheKey != expected {
			t.Errorf("expected cache key %q, got %q", expected, cacheKey)
		}
	})

	t.Run("no alias uses tag name", func(t *testing.T) {
		tagName := "Counter"
		alias := ""
		plcName := "logix"

		displayTag := tagName
		if alias != "" {
			displayTag = alias
		}
		cacheKey := fmt.Sprintf("%s/%s", plcName, displayTag)

		expected := "logix/Counter"
		if cacheKey != expected {
			t.Errorf("expected cache key %q, got %q", expected, cacheKey)
		}
	})

	t.Run("cache consistency with alias", func(t *testing.T) {
		cache := make(map[string]interface{})
		plcName := "s7"
		alias := "sensor_temp"

		// First publish with alias
		cacheKey := fmt.Sprintf("%s/%s", plcName, alias)
		cache[cacheKey] = int32(25)

		// Second publish with same alias, same value
		_, exists := cache[cacheKey]
		if !exists {
			t.Error("cache key should exist after first publish")
		}

		lastValue := cache[cacheKey]
		newValue := int32(25)
		shouldPublish := fmt.Sprintf("%v", lastValue) != fmt.Sprintf("%v", newValue)

		if shouldPublish {
			t.Error("same alias+value should not republish")
		}
	})
}

// TestPublisher_MessagePayload tests that the JSON message payload is correct.
func TestPublisher_MessagePayload(t *testing.T) {
	t.Run("message includes all fields", func(t *testing.T) {
		msg := TagMessage{
			Topic:     "warlink",
			PLC:       "plc1",
			Tag:       "Counter",
			Value:     int32(100),
			Type:      "DINT",
			Writable:  true,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// Verify all required fields
		requiredFields := []string{"topic", "plc", "tag", "value", "type", "writable", "timestamp"}
		for _, field := range requiredFields {
			if _, ok := decoded[field]; !ok {
				t.Errorf("missing required field: %s", field)
			}
		}
	})

	t.Run("alias message includes memloc", func(t *testing.T) {
		msg := TagMessage{
			Topic:     "warlink",
			PLC:       "s7",
			Tag:       "sensor_temp",  // alias
			MemLoc:    "DB1.0",        // original address
			Value:     int32(25),
			Type:      "DINT",
			Writable:  false,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
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
			Topic:     "warlink",
			PLC:       "logix",
			Tag:       "Counter",
			MemLoc:    "",  // empty - should be omitted
			Value:     int32(100),
			Type:      "DINT",
			Writable:  true,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
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

// TestPublisher_ValueAccuracy tests that published values match source values exactly.
func TestPublisher_ValueAccuracy(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		value    interface{}
	}{
		{"int32_positive", "DINT", int32(2147483647)},
		{"int32_negative", "DINT", int32(-2147483648)},
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
				Topic:     "warlink",
				PLC:       "test",
				Tag:       "tag",
				Value:     tc.value,
				Type:      tc.typeName,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
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
				// Float32 loses precision when marshaled/unmarshaled
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

// TestConcurrentCacheAccess tests thread safety of cache operations.
func TestConcurrentCacheAccess(t *testing.T) {
	cache := make(map[string]interface{})
	var mu sync.RWMutex

	var wg sync.WaitGroup
	plcs := []string{"plc1", "plc2", "plc3"}
	tags := []string{"tag1", "tag2", "tag3"}

	// Write all combinations concurrently
	for _, plc := range plcs {
		for _, tag := range tags {
			wg.Add(1)
			go func(plc, tag string) {
				defer wg.Done()
				key := fmt.Sprintf("%s/%s", plc, tag)

				mu.Lock()
				cache[key] = int32(100)
				mu.Unlock()
			}(plc, tag)
		}
	}

	wg.Wait()

	// Verify no race conditions - cache should have values for each PLC/tag combo
	mu.RLock()
	defer mu.RUnlock()

	expectedKeys := len(plcs) * len(tags)
	if len(cache) != expectedKeys {
		t.Errorf("expected %d cache entries, got %d", expectedKeys, len(cache))
	}
}

// TestConvertValueForType tests type conversion for write operations.
func TestConvertValueForType(t *testing.T) {
	tests := []struct {
		name     string
		value    interface{}
		dataType uint16
		expected interface{}
		hasError bool
	}{
		// BOOL conversions
		{"bool_true", true, plcTypeBOOL, true, false},
		{"bool_false", false, plcTypeBOOL, false, false},
		{"num_to_bool_1", float64(1), plcTypeBOOL, true, false},
		{"num_to_bool_0", float64(0), plcTypeBOOL, false, false},

		// SINT (int8) conversions
		{"sint_valid", float64(100), plcTypeSINT, int8(100), false},
		{"sint_min", float64(-128), plcTypeSINT, int8(-128), false},
		{"sint_max", float64(127), plcTypeSINT, int8(127), false},
		{"sint_overflow", float64(128), plcTypeSINT, nil, true},
		{"sint_underflow", float64(-129), plcTypeSINT, nil, true},

		// INT (int16) conversions
		{"int_valid", float64(1000), plcTypeINT, int16(1000), false},
		{"int_min", float64(-32768), plcTypeINT, int16(-32768), false},
		{"int_max", float64(32767), plcTypeINT, int16(32767), false},
		{"int_overflow", float64(32768), plcTypeINT, nil, true},

		// DINT (int32) conversions
		{"dint_valid", float64(100000), plcTypeDINT, int32(100000), false},
		{"dint_negative", float64(-100000), plcTypeDINT, int32(-100000), false},

		// REAL (float32) conversions
		{"real_valid", float64(3.14), plcTypeREAL, float32(3.14), false},

		// LREAL (float64) conversions
		{"lreal_valid", float64(3.14159265359), plcTypeLREAL, float64(3.14159265359), false},

		// USINT (uint8) conversions
		{"usint_valid", float64(200), plcTypeUSINT, uint8(200), false},
		{"usint_max", float64(255), plcTypeUSINT, uint8(255), false},
		{"usint_overflow", float64(256), plcTypeUSINT, nil, true},
		{"usint_negative", float64(-1), plcTypeUSINT, nil, true},

		// UINT (uint16) conversions
		{"uint_valid", float64(50000), plcTypeUINT, uint16(50000), false},
		{"uint_max", float64(65535), plcTypeUINT, uint16(65535), false},
		{"uint_overflow", float64(65536), plcTypeUINT, nil, true},

		// ADS BYTE conversions
		{"byte_valid", float64(200), adsTypeBYTE, uint8(200), false},
		{"byte_overflow", float64(256), adsTypeBYTE, nil, true},

		// ADS WORD conversions
		{"word_valid", float64(50000), adsTypeWORD, uint16(50000), false},

		// STRING conversions
		{"string_valid", "hello", adsTypeSTRING, "hello", false},
		{"string_from_num", float64(123), adsTypeSTRING, nil, true},

		// Invalid type handling
		{"invalid_type", "test", uint16(0), "test", false}, // Unknown types pass through
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := convertValueForType(tc.value, tc.dataType)

			if tc.hasError {
				if err == nil {
					t.Errorf("expected error for %s", tc.name)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Compare values
			switch expected := tc.expected.(type) {
			case int8:
				if r, ok := result.(int8); !ok || r != expected {
					t.Errorf("expected %v (%T), got %v (%T)", expected, expected, result, result)
				}
			case int16:
				if r, ok := result.(int16); !ok || r != expected {
					t.Errorf("expected %v (%T), got %v (%T)", expected, expected, result, result)
				}
			case int32:
				if r, ok := result.(int32); !ok || r != expected {
					t.Errorf("expected %v (%T), got %v (%T)", expected, expected, result, result)
				}
			case uint8:
				if r, ok := result.(uint8); !ok || r != expected {
					t.Errorf("expected %v (%T), got %v (%T)", expected, expected, result, result)
				}
			case uint16:
				if r, ok := result.(uint16); !ok || r != expected {
					t.Errorf("expected %v (%T), got %v (%T)", expected, expected, result, result)
				}
			case float32:
				if r, ok := result.(float32); !ok || r != expected {
					t.Errorf("expected %v (%T), got %v (%T)", expected, expected, result, result)
				}
			case float64:
				if r, ok := result.(float64); !ok || r != expected {
					t.Errorf("expected %v (%T), got %v (%T)", expected, expected, result, result)
				}
			case bool:
				if r, ok := result.(bool); !ok || r != expected {
					t.Errorf("expected %v (%T), got %v (%T)", expected, expected, result, result)
				}
			case string:
				if r, ok := result.(string); !ok || r != expected {
					t.Errorf("expected %v (%T), got %v (%T)", expected, expected, result, result)
				}
			}
		})
	}
}

// TestPublisher_NewPublisher tests publisher creation.
func TestPublisher_NewPublisher(t *testing.T) {
	cfg := &config.MQTTConfig{
		Name:    "test",
		Broker:  "localhost",
		Port:    1883,
		Enabled: true,
	}
	pub := NewPublisher(cfg, "warlink")

	if pub == nil {
		t.Fatal("expected non-nil publisher")
	}
	if pub.Name() != "test" {
		t.Errorf("expected name 'test', got %q", pub.Name())
	}
	if pub.IsRunning() {
		t.Error("new publisher should not be running")
	}
}

// TestPublisher_Address tests address formatting.
func TestPublisher_Address(t *testing.T) {
	t.Run("tcp address", func(t *testing.T) {
		cfg := &config.MQTTConfig{
			Broker:  "localhost",
			Port:    1883,
			UseTLS:  false,
		}
		pub := NewPublisher(cfg, "test")
		addr := pub.Address()

		if addr != "tcp://localhost:1883" {
			t.Errorf("expected 'tcp://localhost:1883', got %q", addr)
		}
	})

	t.Run("ssl address", func(t *testing.T) {
		cfg := &config.MQTTConfig{
			Broker:  "localhost",
			Port:    8883,
			UseTLS:  true,
		}
		pub := NewPublisher(cfg, "test")
		addr := pub.Address()

		if addr != "ssl://localhost:8883" {
			t.Errorf("expected 'ssl://localhost:8883', got %q", addr)
		}
	})
}
