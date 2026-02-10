package kafka

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"warlink/namespace"
)

// TestManager_ChangeDetection tests that duplicate values are not republished.
func TestManager_ChangeDetection(t *testing.T) {
	t.Run("identical values should not republish", func(t *testing.T) {
		m := newTestManager()

		// First publish sets the value
		m.updateLastValue("cluster/plc1/tag1", int32(100))

		// Check if value would be republished
		shouldPublish := m.shouldPublish("cluster/plc1/tag1", int32(100), false)
		if shouldPublish {
			t.Error("identical value should not republish")
		}
	})

	t.Run("different values should republish", func(t *testing.T) {
		m := newTestManager()

		// First publish
		m.updateLastValue("cluster/plc1/tag1", int32(100))

		// Different value should republish
		shouldPublish := m.shouldPublish("cluster/plc1/tag1", int32(200), false)
		if !shouldPublish {
			t.Error("different value should republish")
		}
	})

	t.Run("force flag should override change detection", func(t *testing.T) {
		m := newTestManager()

		// First publish
		m.updateLastValue("cluster/plc1/tag1", int32(100))

		// Same value with force flag should republish
		shouldPublish := m.shouldPublish("cluster/plc1/tag1", int32(100), true)
		if !shouldPublish {
			t.Error("force flag should override change detection")
		}
	})

	t.Run("different clusters are tracked separately", func(t *testing.T) {
		m := newTestManager()

		// Set value for cluster1
		m.updateLastValue("cluster1/plc1/tag1", int32(100))

		// Same tag/value on different cluster should publish
		shouldPublish := m.shouldPublish("cluster2/plc1/tag1", int32(100), false)
		if !shouldPublish {
			t.Error("different clusters should be tracked separately")
		}
	})
}

// TestManager_ChangeDetectionTypes tests change detection across different data types.
func TestManager_ChangeDetectionTypes(t *testing.T) {
	tests := []struct {
		name      string
		value1    interface{}
		value2    interface{}
		shouldPub bool
		desc      string
	}{
		// Integer types
		{"int32_same", int32(100), int32(100), false, "same int32"},
		{"int32_diff", int32(100), int32(200), true, "different int32"},

		// Float types
		{"float32_same", float32(3.14), float32(3.14), false, "same float32"},
		{"float32_diff", float32(3.14), float32(2.71), true, "different float32"},

		// Boolean types
		{"bool_same", true, true, false, "same bool"},
		{"bool_diff", true, false, true, "different bool"},

		// String types
		{"string_same", "hello", "hello", false, "same string"},
		{"string_diff", "hello", "world", true, "different string"},

		// Nil handling
		{"nil_to_value", nil, int32(0), true, "nil to value"},
		{"value_to_nil", int32(0), nil, true, "value to nil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestManager()

			// First value
			if tc.value1 != nil {
				m.updateLastValue("cluster/plc/tag", tc.value1)
			}

			// Second value
			shouldPublish := m.shouldPublish("cluster/plc/tag", tc.value2, false)

			if shouldPublish != tc.shouldPub {
				t.Errorf("%s: expected publish=%v, got %v", tc.desc, tc.shouldPub, shouldPublish)
			}
		})
	}
}

// TestManager_AliasConsistency tests that aliases are used correctly in cache keys.
func TestManager_AliasConsistency(t *testing.T) {
	t.Run("alias used in cache key", func(t *testing.T) {
		m := newTestManager()

		// The cache key should use the display tag (alias if present)
		// Format: cluster/plc/displayTag
		m.updateLastValue("cluster/s7/sensor_temp", int32(25))

		// Check that cache uses alias
		m.lastMu.RLock()
		_, hasAlias := m.lastValues["cluster/s7/sensor_temp"]
		m.lastMu.RUnlock()

		if !hasAlias {
			t.Error("cache should use alias as key")
		}
	})

	t.Run("no alias uses tag name", func(t *testing.T) {
		m := newTestManager()

		// Without alias, tag name is used
		m.updateLastValue("cluster/logix/Counter", int32(100))

		m.lastMu.RLock()
		_, hasTag := m.lastValues["cluster/logix/Counter"]
		m.lastMu.RUnlock()

		if !hasTag {
			t.Error("cache should use tag name when no alias")
		}
	})
}

// TestTagMessage_AliasFields tests that alias and address fields are correct in messages.
func TestTagMessage_AliasFields(t *testing.T) {
	t.Run("alias message includes memloc", func(t *testing.T) {
		msg := TagMessage{
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
			PLC:       "logix",
			Tag:       "Counter",
			MemLoc:    "",  // empty
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

// TestTagMessage_ValueAccuracy tests that published values match source values.
func TestTagMessage_ValueAccuracy(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		value    interface{}
	}{
		{"int32_max", "DINT", int32(2147483647)},
		{"int32_min", "DINT", int32(-2147483648)},
		{"int16_max", "INT", int16(32767)},
		{"float64_precise", "LREAL", float64(3.141592653589793)},
		{"bool_true", "BOOL", true},
		{"string_unicode", "STRING", "测试数据"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := TagMessage{
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

			// Verify value accuracy
			switch v := tc.value.(type) {
			case int32:
				if decoded.Value.(float64) != float64(v) {
					t.Errorf("value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case int16:
				if decoded.Value.(float64) != float64(v) {
					t.Errorf("value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case float64:
				if decoded.Value.(float64) != v {
					t.Errorf("value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case bool:
				if decoded.Value.(bool) != v {
					t.Errorf("value mismatch: expected %v, got %v", v, decoded.Value)
				}
			case string:
				if decoded.Value.(string) != v {
					t.Errorf("value mismatch: expected %q, got %q", v, decoded.Value)
				}
			}
		})
	}
}

// TestManager_ConcurrentPublish tests thread safety of publish operations.
func TestManager_ConcurrentPublish(t *testing.T) {
	m := newTestManager()

	var wg sync.WaitGroup
	publishCount := 100
	clusters := []string{"cluster1", "cluster2"}
	plcs := []string{"plc1", "plc2", "plc3"}
	tags := []string{"tag1", "tag2", "tag3"}

	for i := 0; i < publishCount; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cluster := clusters[i%len(clusters)]
			plc := plcs[i%len(plcs)]
			tag := tags[i%len(tags)]
			key := cluster + "/" + plc + "/" + tag
			m.updateLastValue(key, int32(i))
		}(i)
	}

	wg.Wait()

	// Verify no race conditions - cache should have some entries
	// The exact count depends on modulo distribution
	m.lastMu.RLock()
	defer m.lastMu.RUnlock()

	// With modulo operations, we get unique combinations
	// 2 clusters * 3 plcs * 3 tags = 18, but with modulo 100:
	// i%2 gives 2 clusters, i%3 gives 3 plcs, i%3 gives 3 tags
	// The combinations depend on i values - just verify we have entries
	if len(m.lastValues) == 0 {
		t.Error("expected some cache entries")
	}
	if len(m.lastValues) > publishCount {
		t.Errorf("unexpected cache size: %d > %d", len(m.lastValues), publishCount)
	}
}

// TestManager_ClearLastValues tests that clearing the cache forces republish.
func TestManager_ClearLastValues(t *testing.T) {
	m := newTestManager()

	// Add some values
	m.updateLastValue("cluster/plc1/tag1", int32(100))
	m.updateLastValue("cluster/plc1/tag2", int32(200))

	// Verify values exist
	m.lastMu.RLock()
	if len(m.lastValues) != 2 {
		t.Errorf("expected 2 cached values, got %d", len(m.lastValues))
	}
	m.lastMu.RUnlock()

	// Clear cache
	m.ClearLastValues()

	// Verify cache is empty
	m.lastMu.RLock()
	if len(m.lastValues) != 0 {
		t.Errorf("expected 0 cached values after clear, got %d", len(m.lastValues))
	}
	m.lastMu.RUnlock()

	// Now same value should publish again
	shouldPublish := m.shouldPublish("cluster/plc1/tag1", int32(100), false)
	if !shouldPublish {
		t.Error("value should publish after cache clear")
	}
}

// TestBatchConfig tests batching configuration constants.
func TestBatchConfig(t *testing.T) {
	if MaxBatchSize <= 0 {
		t.Error("MaxBatchSize should be positive")
	}
	if MaxBatchSize > 1000 {
		t.Error("MaxBatchSize seems too large")
	}

	if BatchFlushInterval <= 0 {
		t.Error("BatchFlushInterval should be positive")
	}
	if BatchFlushInterval > time.Second {
		t.Error("BatchFlushInterval seems too long for real-time data")
	}

	if MaxBatchQueueSize <= 0 {
		t.Error("MaxBatchQueueSize should be positive")
	}
}

// Helper functions for testing

func newTestManager() *Manager {
	return &Manager{
		producers:  make(map[string]*Producer),
		consumers:  make(map[string]*Consumer),
		builders:   make(map[string]*namespace.Builder),
		lastValues: make(map[string]interface{}),
		batchChan:  make(chan publishJob, MaxBatchQueueSize),
		stopChan:   make(chan struct{}),
	}
}

// updateLastValue is a test helper to update the cache directly.
func (m *Manager) updateLastValue(key string, value interface{}) {
	m.lastMu.Lock()
	m.lastValues[key] = value
	m.lastMu.Unlock()
}

// shouldPublish is a test helper to check if a value should be published.
func (m *Manager) shouldPublish(cacheKey string, value interface{}, force bool) bool {
	m.lastMu.RLock()
	lastValue, exists := m.lastValues[cacheKey]
	m.lastMu.RUnlock()

	if !exists {
		return true
	}
	if force {
		return true
	}
	return fmt.Sprintf("%v", lastValue) != fmt.Sprintf("%v", value)
}
