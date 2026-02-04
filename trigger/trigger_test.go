package trigger

import (
	"encoding/json"
	"testing"

	"warlogix/config"
)

func TestStatus_String(t *testing.T) {
	tests := []struct {
		status   Status
		expected string
	}{
		{StatusDisabled, "Disabled"},
		{StatusArmed, "Armed"},
		{StatusFiring, "Firing"},
		{StatusCooldown, "Cooldown"},
		{StatusError, "Error"},
		{Status(99), "Unknown"},
	}

	for _, tc := range tests {
		result := tc.status.String()
		if result != tc.expected {
			t.Errorf("Status(%d).String() = %q, want %q", tc.status, result, tc.expected)
		}
	}
}

func TestParseOperator(t *testing.T) {
	t.Run("valid operators", func(t *testing.T) {
		tests := []struct {
			input    string
			expected Operator
		}{
			{"==", OpEqual},
			{"!=", OpNotEqual},
			{">", OpGreater},
			{"<", OpLess},
			{">=", OpGreaterEqual},
			{"<=", OpLessEqual},
		}

		for _, tc := range tests {
			op, err := ParseOperator(tc.input)
			if err != nil {
				t.Errorf("ParseOperator(%q) error: %v", tc.input, err)
			}
			if op != tc.expected {
				t.Errorf("ParseOperator(%q) = %q, want %q", tc.input, op, tc.expected)
			}
		}
	})

	t.Run("invalid operator", func(t *testing.T) {
		_, err := ParseOperator("invalid")
		if err == nil {
			t.Error("expected error for invalid operator")
		}
	})
}

func TestValidOperators(t *testing.T) {
	ops := ValidOperators()
	if len(ops) != 6 {
		t.Errorf("expected 6 operators, got %d", len(ops))
	}

	expected := []string{"==", "!=", ">", "<", ">=", "<="}
	for i, op := range expected {
		if ops[i] != op {
			t.Errorf("operator %d: expected %q, got %q", i, op, ops[i])
		}
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected float64
		ok       bool
	}{
		{"float64", float64(3.14), 3.14, true},
		{"float32", float32(2.5), 2.5, true},
		{"int", int(42), 42, true},
		{"int8", int8(8), 8, true},
		{"int16", int16(16), 16, true},
		{"int32", int32(32), 32, true},
		{"int64", int64(64), 64, true},
		{"uint", uint(100), 100, true},
		{"uint8", uint8(8), 8, true},
		{"uint16", uint16(16), 16, true},
		{"uint32", uint32(32), 32, true},
		{"uint64", uint64(64), 64, true},
		{"bool true", true, 1, true},
		{"bool false", false, 0, true},
		{"string number", "123.45", 123.45, true},
		{"string non-number", "hello", 0, false},
		{"nil", nil, 0, false},
		{"struct", struct{}{}, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := toFloat64(tc.input)
			if ok != tc.ok {
				t.Errorf("toFloat64(%v) ok = %v, want %v", tc.input, ok, tc.ok)
			}
			if ok && result != tc.expected {
				t.Errorf("toFloat64(%v) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

func TestCondition_Evaluate(t *testing.T) {
	t.Run("numeric comparisons", func(t *testing.T) {
		tests := []struct {
			op       Operator
			target   interface{}
			value    interface{}
			expected bool
		}{
			// Equal
			{OpEqual, 10, 10, true},
			{OpEqual, 10, 20, false},
			{OpEqual, 10.0, int(10), true},

			// Not Equal
			{OpNotEqual, 10, 20, true},
			{OpNotEqual, 10, 10, false},

			// Greater
			{OpGreater, 10, 20, true},
			{OpGreater, 10, 10, false},
			{OpGreater, 10, 5, false},

			// Less
			{OpLess, 10, 5, true},
			{OpLess, 10, 10, false},
			{OpLess, 10, 20, false},

			// Greater Equal
			{OpGreaterEqual, 10, 10, true},
			{OpGreaterEqual, 10, 20, true},
			{OpGreaterEqual, 10, 5, false},

			// Less Equal
			{OpLessEqual, 10, 10, true},
			{OpLessEqual, 10, 5, true},
			{OpLessEqual, 10, 20, false},

			// Mixed types
			{OpEqual, int32(100), float64(100), true},
			{OpGreater, float32(5.5), int(10), true},
		}

		for _, tc := range tests {
			cond := &Condition{Operator: tc.op, Value: tc.target}
			result, err := cond.Evaluate(tc.value)
			if err != nil {
				t.Errorf("Evaluate(%v %s %v) error: %v", tc.value, tc.op, tc.target, err)
			}
			if result != tc.expected {
				t.Errorf("Evaluate(%v %s %v) = %v, want %v", tc.value, tc.op, tc.target, result, tc.expected)
			}
		}
	})

	t.Run("boolean comparisons", func(t *testing.T) {
		cond := &Condition{Operator: OpEqual, Value: true}
		result, err := cond.Evaluate(true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result {
			t.Error("expected true == true to be true")
		}

		result, err = cond.Evaluate(false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result {
			t.Error("expected false == true to be false")
		}
	})

	t.Run("non-numeric with unsupported operator", func(t *testing.T) {
		cond := &Condition{Operator: OpGreater, Value: "hello"}
		_, err := cond.Evaluate("world")
		if err == nil {
			t.Error("expected error for > with strings")
		}
	})

	t.Run("string equality", func(t *testing.T) {
		cond := &Condition{Operator: OpEqual, Value: "hello"}
		result, err := cond.Evaluate("hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result {
			t.Error("expected 'hello' == 'hello' to be true")
		}
	})
}

func TestNewMessage(t *testing.T) {
	metadata := map[string]string{"line": "Line1"}
	data := map[string]interface{}{"Counter": 100, "Status": true}

	msg := NewMessage("TestTrigger", "MainPLC", metadata, data)

	if msg.Trigger != "TestTrigger" {
		t.Errorf("expected Trigger 'TestTrigger', got %s", msg.Trigger)
	}
	if msg.PLC != "MainPLC" {
		t.Errorf("expected PLC 'MainPLC', got %s", msg.PLC)
	}
	if msg.Sequence == 0 {
		t.Error("expected non-zero Sequence")
	}
	if msg.Timestamp == "" {
		t.Error("expected non-empty Timestamp")
	}
	if msg.Metadata["line"] != "Line1" {
		t.Error("metadata not preserved")
	}
	if msg.Data["Counter"] != 100 {
		t.Error("data not preserved")
	}
}

func TestMessage_ToJSON(t *testing.T) {
	msg := NewMessage("Test", "PLC1", nil, map[string]interface{}{"value": 42})

	jsonData, err := msg.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Verify it's valid JSON
	var decoded Message
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if decoded.Trigger != "Test" {
		t.Error("Trigger not preserved in JSON")
	}
	if decoded.PLC != "PLC1" {
		t.Error("PLC not preserved in JSON")
	}
}

func TestMessage_Key(t *testing.T) {
	msg := NewMessage("ProductComplete", "MainPLC", nil, nil)

	key := msg.Key()
	expected := "MainPLC:ProductComplete"

	if string(key) != expected {
		t.Errorf("expected key %q, got %q", expected, string(key))
	}
}

func TestNewTrigger(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &config.TriggerConfig{
			Name:       "Test",
			PLC:        "MainPLC",
			TriggerTag: "Ready",
			Condition:  config.TriggerCondition{Operator: "==", Value: true},
			Tags:       []string{"Counter", "Status"},
		}

		trigger, err := NewTrigger(cfg, nil, nil, nil)
		if err != nil {
			t.Fatalf("NewTrigger failed: %v", err)
		}
		if trigger == nil {
			t.Fatal("expected non-nil trigger")
		}
		if trigger.GetStatus() != StatusDisabled {
			t.Error("expected initial status Disabled")
		}
	})

	t.Run("invalid operator", func(t *testing.T) {
		cfg := &config.TriggerConfig{
			Name:       "Test",
			Condition:  config.TriggerCondition{Operator: "invalid", Value: true},
		}

		_, err := NewTrigger(cfg, nil, nil, nil)
		if err == nil {
			t.Error("expected error for invalid operator")
		}
	})
}

func TestTrigger_StartStop(t *testing.T) {
	cfg := &config.TriggerConfig{
		Name:       "Test",
		Enabled:    true,
		PLC:        "MainPLC",
		TriggerTag: "Ready",
		Condition:  config.TriggerCondition{Operator: "==", Value: true},
	}

	// Create a mock reader that returns false
	reader := &mockTagReader{values: map[string]interface{}{"Ready": false}}

	trigger, err := NewTrigger(cfg, nil, reader, nil)
	if err != nil {
		t.Fatalf("NewTrigger failed: %v", err)
	}

	// Start trigger
	trigger.Start()

	// Give it a moment to start
	status := trigger.GetStatus()
	if status != StatusArmed {
		t.Errorf("expected status Armed after start, got %s", status)
	}

	// Stop trigger
	trigger.Stop()

	status = trigger.GetStatus()
	if status != StatusDisabled {
		t.Errorf("expected status Disabled after stop, got %s", status)
	}
}

func TestTrigger_StartDisabled(t *testing.T) {
	cfg := &config.TriggerConfig{
		Name:       "Test",
		Enabled:    false, // Disabled
		PLC:        "MainPLC",
		TriggerTag: "Ready",
		Condition:  config.TriggerCondition{Operator: "==", Value: true},
	}

	trigger, _ := NewTrigger(cfg, nil, nil, nil)
	trigger.Start()

	if trigger.GetStatus() != StatusDisabled {
		t.Error("disabled trigger should stay disabled after Start")
	}
}

func TestTrigger_Reset(t *testing.T) {
	cfg := &config.TriggerConfig{
		Name:       "Test",
		Enabled:    true,
		PLC:        "MainPLC",
		TriggerTag: "Ready",
		Condition:  config.TriggerCondition{Operator: "==", Value: true},
	}

	trigger, _ := NewTrigger(cfg, nil, nil, nil)

	// Set to error state manually
	trigger.mu.Lock()
	trigger.status = StatusError
	trigger.mu.Unlock()

	// Reset should move to armed
	trigger.Reset()

	if trigger.GetStatus() != StatusArmed {
		t.Errorf("expected status Armed after Reset, got %s", trigger.GetStatus())
	}
}

func TestTrigger_GetStats(t *testing.T) {
	cfg := &config.TriggerConfig{
		Name:       "Test",
		Enabled:    true,
		Condition:  config.TriggerCondition{Operator: "==", Value: true},
	}

	trigger, _ := NewTrigger(cfg, nil, nil, nil)

	count, lastFire := trigger.GetStats()
	if count != 0 {
		t.Errorf("expected fire count 0, got %d", count)
	}
	if !lastFire.IsZero() {
		t.Error("expected zero time for lastFire")
	}
}

// mockTagReader is a simple mock for testing
type mockTagReader struct {
	values map[string]interface{}
}

func (m *mockTagReader) ReadTag(plcName, tagName string) (interface{}, error) {
	if v, ok := m.values[tagName]; ok {
		return v, nil
	}
	return nil, nil
}

func (m *mockTagReader) ReadTags(plcName string, tagNames []string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for _, name := range tagNames {
		if v, ok := m.values[name]; ok {
			result[name] = v
		}
	}
	return result, nil
}
