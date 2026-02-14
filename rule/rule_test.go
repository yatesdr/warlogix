package rule

import (
	"encoding/json"
	"testing"

	"warlink/config"
)

func TestStatus_String(t *testing.T) {
	tests := []struct {
		status   Status
		expected string
	}{
		{StatusDisabled, "Stopped"},
		{StatusArmed, "Armed"},
		{StatusFiring, "Firing"},
		{StatusWaitingClear, "Waiting Clear"},
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
}

func TestCondition_Evaluate(t *testing.T) {
	t.Run("numeric comparisons", func(t *testing.T) {
		tests := []struct {
			op       Operator
			target   interface{}
			value    interface{}
			expected bool
		}{
			{OpEqual, 10, 10, true},
			{OpEqual, 10, 20, false},
			{OpNotEqual, 10, 20, true},
			{OpNotEqual, 10, 10, false},
			{OpGreater, 10, 20, true},
			{OpGreater, 10, 5, false},
			{OpLess, 10, 5, true},
			{OpLess, 10, 20, false},
			{OpGreaterEqual, 10, 10, true},
			{OpGreaterEqual, 10, 20, true},
			{OpGreaterEqual, 10, 5, false},
			{OpLessEqual, 10, 10, true},
			{OpLessEqual, 10, 5, true},
			{OpLessEqual, 10, 20, false},
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

	t.Run("NOT modifier", func(t *testing.T) {
		cond := &Condition{Operator: OpEqual, Value: 10, Not: true}
		result, err := cond.Evaluate(10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result {
			t.Error("NOT (10 == 10) should be false")
		}

		result, err = cond.Evaluate(20)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result {
			t.Error("NOT (20 == 10) should be true")
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

	t.Run("non-numeric with unsupported operator", func(t *testing.T) {
		cond := &Condition{Operator: OpGreater, Value: "hello"}
		_, err := cond.Evaluate("world")
		if err == nil {
			t.Error("expected error for > with strings")
		}
	})
}

func TestNewMessage(t *testing.T) {
	data := map[string]interface{}{"Counter": 100, "Status": true}
	triggerInfo := map[string]interface{}{"PLC1.Ready": true}

	msg := NewMessage("TestRule", "MainPLC", triggerInfo, data)

	if msg.Rule != "TestRule" {
		t.Errorf("expected Rule 'TestRule', got %s", msg.Rule)
	}
	if msg.PLC != "MainPLC" {
		t.Errorf("expected PLC 'MainPLC', got %s", msg.PLC)
	}
	if msg.Sequence == 0 {
		t.Error("expected non-zero Sequence")
	}
	if msg.Data["Counter"] != 100 {
		t.Error("data not preserved")
	}
	if msg.Trigger["PLC1.Ready"] != true {
		t.Error("trigger info not preserved")
	}
}

func TestMessage_ToJSON(t *testing.T) {
	msg := NewMessage("Test", "PLC1", nil, map[string]interface{}{"value": 42})

	jsonData, err := msg.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	var decoded Message
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("failed to decode JSON: %v", err)
	}

	if decoded.Rule != "Test" {
		t.Error("Rule not preserved in JSON")
	}
}

func TestMessage_Key(t *testing.T) {
	msg := NewMessage("alarm-1", "PLC1", nil, nil)
	if string(msg.Key()) != "alarm-1" {
		t.Errorf("expected key 'alarm-1', got %q", string(msg.Key()))
	}
}

func TestNewRule(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &config.RuleConfig{
			Name:    "Test",
			Enabled: true,
			Conditions: []config.RuleCondition{
				{PLC: "PLC1", Tag: "Ready", Operator: "==", Value: true},
			},
			Actions: []config.RuleAction{
				{Type: config.ActionWriteback, WriteTag: "Ack", WriteValue: 1},
			},
		}

		rule, err := NewRule(cfg, nil, nil, nil)
		if err != nil {
			t.Fatalf("NewRule failed: %v", err)
		}
		if rule.GetStatus() != StatusDisabled {
			t.Error("expected initial status Stopped")
		}
	})

	t.Run("invalid operator", func(t *testing.T) {
		cfg := &config.RuleConfig{
			Name: "Test",
			Conditions: []config.RuleCondition{
				{PLC: "PLC1", Tag: "Ready", Operator: "invalid", Value: true},
			},
		}
		_, err := NewRule(cfg, nil, nil, nil)
		if err == nil {
			t.Error("expected error for invalid operator")
		}
	})

	t.Run("NOT condition", func(t *testing.T) {
		cfg := &config.RuleConfig{
			Name: "Test",
			Conditions: []config.RuleCondition{
				{PLC: "PLC1", Tag: "Valve", Operator: "==", Value: 0, Not: true},
			},
		}
		rule, err := NewRule(cfg, nil, nil, nil)
		if err != nil {
			t.Fatalf("NewRule failed: %v", err)
		}
		if !rule.conditions[0].Not {
			t.Error("expected condition Not=true")
		}
	})
}

func TestRule_StartStop(t *testing.T) {
	cfg := &config.RuleConfig{
		Name:    "Test",
		Enabled: true,
		Conditions: []config.RuleCondition{
			{PLC: "PLC1", Tag: "Ready", Operator: "==", Value: true},
		},
	}

	reader := &mockTagReader{values: map[string]interface{}{"Ready": false}}
	rule, err := NewRule(cfg, nil, reader, nil)
	if err != nil {
		t.Fatalf("NewRule failed: %v", err)
	}

	rule.Start()
	if rule.GetStatus() != StatusArmed {
		t.Errorf("expected Armed after start, got %s", rule.GetStatus())
	}

	rule.Stop()
	if rule.GetStatus() != StatusDisabled {
		t.Errorf("expected Stopped after stop, got %s", rule.GetStatus())
	}
}

func TestRule_StartDisabled(t *testing.T) {
	cfg := &config.RuleConfig{
		Name:    "Test",
		Enabled: false,
		Conditions: []config.RuleCondition{
			{PLC: "PLC1", Tag: "Ready", Operator: "==", Value: true},
		},
	}

	rule, _ := NewRule(cfg, nil, nil, nil)
	rule.Start()
	if rule.GetStatus() != StatusDisabled {
		t.Error("disabled rule should stay disabled after Start")
	}
}

func TestRule_Reset(t *testing.T) {
	cfg := &config.RuleConfig{
		Name:    "Test",
		Enabled: true,
		Conditions: []config.RuleCondition{
			{PLC: "PLC1", Tag: "Ready", Operator: "==", Value: true},
		},
	}

	rule, _ := NewRule(cfg, nil, nil, nil)

	rule.mu.Lock()
	rule.status = StatusError
	rule.mu.Unlock()

	rule.Reset()
	if rule.GetStatus() != StatusArmed {
		t.Errorf("expected Armed after Reset, got %s", rule.GetStatus())
	}
}

func TestRule_GetStats(t *testing.T) {
	cfg := &config.RuleConfig{
		Name: "Test",
		Conditions: []config.RuleCondition{
			{PLC: "PLC1", Tag: "Ready", Operator: "==", Value: true},
		},
	}

	rule, _ := NewRule(cfg, nil, nil, nil)
	count, lastFire := rule.GetStats()
	if count != 0 {
		t.Errorf("expected fire count 0, got %d", count)
	}
	if !lastFire.IsZero() {
		t.Error("expected zero time for lastFire")
	}
}

// mockTagReader is a simple mock for testing.
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
