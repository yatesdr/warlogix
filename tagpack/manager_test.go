package tagpack

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"warlink/config"
)

// TestPackValue_Structure tests the PackValue JSON structure.
func TestPackValue_Structure(t *testing.T) {
	t.Run("basic pack value", func(t *testing.T) {
		pv := PackValue{
			Name:      "test_pack",
			Timestamp: time.Now().UTC(),
			Tags: map[string]TagData{
				"plc1.Counter": {
					Value:  int32(100),
					Type:   "DINT",
					PLC:    "plc1",
					MemLoc: "",
				},
			},
		}

		data, err := json.Marshal(pv)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// Verify required fields
		if decoded["name"] != "test_pack" {
			t.Errorf("expected name 'test_pack', got %v", decoded["name"])
		}
		if _, ok := decoded["timestamp"]; !ok {
			t.Error("missing timestamp field")
		}
		if _, ok := decoded["tags"]; !ok {
			t.Error("missing tags field")
		}
	})

	t.Run("pack with PLC errors", func(t *testing.T) {
		pv := PackValue{
			Name:      "test_pack",
			Timestamp: time.Now().UTC(),
			Tags: map[string]TagData{
				"plc1.Counter": {
					Value:  nil,
					Type:   "",
					PLC:    "plc1",
					MemLoc: "",
				},
			},
			PLCs: map[string]PLCMetadata{
				"plc1": {
					Address:   "192.168.1.1",
					Family:    "logix",
					Connected: false,
					Error:     "connection refused",
				},
			},
		}

		data, err := json.Marshal(pv)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// PLCs should be present when there are errors
		if _, ok := decoded["plcs"]; !ok {
			t.Error("expected plcs field when there are errors")
		}
	})

	t.Run("pack without errors omits PLCs", func(t *testing.T) {
		pv := PackValue{
			Name:      "test_pack",
			Timestamp: time.Now().UTC(),
			Tags: map[string]TagData{
				"plc1.Counter": {
					Value:  int32(100),
					Type:   "DINT",
					PLC:    "plc1",
					MemLoc: "",
				},
			},
			PLCs: nil, // No errors
		}

		data, err := json.Marshal(pv)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// PLCs should be omitted when empty
		if _, ok := decoded["plcs"]; ok {
			t.Error("plcs field should be omitted when empty")
		}
	})
}

// TestTagData_AliasHandling tests that aliases are handled correctly in TagData.
func TestTagData_AliasHandling(t *testing.T) {
	t.Run("alias uses memloc for original address", func(t *testing.T) {
		td := TagData{
			Value:  int32(25),
			Type:   "DINT",
			PLC:    "s7",
			MemLoc: "DB1.0", // Original S7 address
		}

		data, err := json.Marshal(td)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if decoded["memloc"] != "DB1.0" {
			t.Errorf("expected memloc 'DB1.0', got %v", decoded["memloc"])
		}
	})

	t.Run("non-alias omits memloc", func(t *testing.T) {
		td := TagData{
			Value:  int32(100),
			Type:   "DINT",
			PLC:    "logix",
			MemLoc: "", // No alias
		}

		data, err := json.Marshal(td)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded map[string]interface{}
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if _, ok := decoded["memloc"]; ok {
			t.Error("memloc should be omitted when empty")
		}
	})
}

// TestPackValue_AliasConsistency tests alias handling in pack values.
func TestPackValue_AliasConsistency(t *testing.T) {
	t.Run("mixed alias and non-alias tags", func(t *testing.T) {
		pv := PackValue{
			Name:      "mixed_pack",
			Timestamp: time.Now().UTC(),
			Tags: map[string]TagData{
				// S7 tag with alias - key uses alias
				"s7.sensor_temp": {
					Value:  int32(25),
					Type:   "DINT",
					PLC:    "s7",
					MemLoc: "DB1.0", // Original address in memloc
				},
				// Logix tag without alias - key uses tag name
				"logix.Counter": {
					Value:  int32(100),
					Type:   "DINT",
					PLC:    "logix",
					MemLoc: "", // No memloc for non-aliased tags
				},
			},
		}

		data, err := json.Marshal(pv)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var decoded PackValue
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		// Verify S7 aliased tag
		s7Tag, ok := decoded.Tags["s7.sensor_temp"]
		if !ok {
			t.Error("expected key 's7.sensor_temp' for aliased tag")
		}
		if s7Tag.MemLoc != "DB1.0" {
			t.Errorf("expected memloc 'DB1.0', got %q", s7Tag.MemLoc)
		}

		// Verify Logix non-aliased tag
		logixTag, ok := decoded.Tags["logix.Counter"]
		if !ok {
			t.Error("expected key 'logix.Counter' for non-aliased tag")
		}
		if logixTag.MemLoc != "" {
			t.Errorf("expected empty memloc for non-aliased tag, got %q", logixTag.MemLoc)
		}
	})
}

// TestManager_OnTagChanges tests that tag changes trigger the correct packs.
func TestManager_OnTagChanges(t *testing.T) {
	t.Run("change triggers pack", func(t *testing.T) {
		cfg := &config.Config{
			Namespace: "test",
			TagPacks: []config.TagPackConfig{
				{
					Name:    "pack1",
					Enabled: true,
					Members: []config.TagPackMember{
						{PLC: "plc1", Tag: "tag1"},
						{PLC: "plc1", Tag: "tag2"},
					},
				},
			},
		}

		provider := &mockDataProvider{
			values: map[string]mockTagValue{
				"plc1/tag1": {value: int32(100), typeName: "DINT"},
				"plc1/tag2": {value: int32(200), typeName: "DINT"},
			},
		}

		m := NewManager(cfg, provider)
		defer m.Stop()

		var published []string
		var mu sync.Mutex

		m.SetOnPublish(func(info PackPublishInfo) {
			mu.Lock()
			published = append(published, info.Value.Name)
			mu.Unlock()
		})

		// Trigger change
		m.OnTagChanges("plc1", []string{"tag1"})

		// Wait for debounce
		time.Sleep(300 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		if len(published) != 1 {
			t.Errorf("expected 1 pack published, got %d", len(published))
		}
		if len(published) > 0 && published[0] != "pack1" {
			t.Errorf("expected 'pack1', got %q", published[0])
		}
	})

	t.Run("ignored member does not trigger pack", func(t *testing.T) {
		cfg := &config.Config{
			Namespace: "test",
			TagPacks: []config.TagPackConfig{
				{
					Name:    "pack1",
					Enabled: true,
					Members: []config.TagPackMember{
						{PLC: "plc1", Tag: "tag1", IgnoreChanges: true},
						{PLC: "plc1", Tag: "tag2"},
					},
				},
			},
		}

		provider := &mockDataProvider{
			values: map[string]mockTagValue{
				"plc1/tag1": {value: int32(100), typeName: "DINT"},
				"plc1/tag2": {value: int32(200), typeName: "DINT"},
			},
		}

		m := NewManager(cfg, provider)
		defer m.Stop()

		var published int
		var mu sync.Mutex

		m.SetOnPublish(func(info PackPublishInfo) {
			mu.Lock()
			published++
			mu.Unlock()
		})

		// Trigger change on ignored tag
		m.OnTagChanges("plc1", []string{"tag1"})

		// Wait for debounce
		time.Sleep(300 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		if published != 0 {
			t.Errorf("expected 0 packs published (ignored tag), got %d", published)
		}
	})

	t.Run("disabled pack does not trigger", func(t *testing.T) {
		cfg := &config.Config{
			Namespace: "test",
			TagPacks: []config.TagPackConfig{
				{
					Name:    "pack1",
					Enabled: false, // Disabled
					Members: []config.TagPackMember{
						{PLC: "plc1", Tag: "tag1"},
					},
				},
			},
		}

		provider := &mockDataProvider{}
		m := NewManager(cfg, provider)
		defer m.Stop()

		var published int
		var mu sync.Mutex

		m.SetOnPublish(func(info PackPublishInfo) {
			mu.Lock()
			published++
			mu.Unlock()
		})

		// Trigger change
		m.OnTagChanges("plc1", []string{"tag1"})

		// Wait for debounce
		time.Sleep(300 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		if published != 0 {
			t.Errorf("expected 0 packs published (disabled pack), got %d", published)
		}
	})
}

// TestManager_Debounce tests the debounce behavior.
func TestManager_Debounce(t *testing.T) {
	t.Run("multiple changes within debounce window", func(t *testing.T) {
		cfg := &config.Config{
			Namespace: "test",
			TagPacks: []config.TagPackConfig{
				{
					Name:    "pack1",
					Enabled: true,
					Members: []config.TagPackMember{
						{PLC: "plc1", Tag: "tag1"},
						{PLC: "plc1", Tag: "tag2"},
					},
				},
			},
		}

		provider := &mockDataProvider{
			values: map[string]mockTagValue{
				"plc1/tag1": {value: int32(100), typeName: "DINT"},
				"plc1/tag2": {value: int32(200), typeName: "DINT"},
			},
		}

		m := NewManager(cfg, provider)
		defer m.Stop()

		var published int
		var mu sync.Mutex

		m.SetOnPublish(func(info PackPublishInfo) {
			mu.Lock()
			published++
			mu.Unlock()
		})

		// Trigger multiple changes rapidly
		m.OnTagChanges("plc1", []string{"tag1"})
		time.Sleep(50 * time.Millisecond)
		m.OnTagChanges("plc1", []string{"tag2"})
		time.Sleep(50 * time.Millisecond)
		m.OnTagChanges("plc1", []string{"tag1"})

		// Wait for debounce to complete
		time.Sleep(350 * time.Millisecond)

		mu.Lock()
		defer mu.Unlock()

		// Should only publish once due to debounce
		if published != 1 {
			t.Errorf("expected 1 publish (debounced), got %d", published)
		}
	})
}

// TestManager_PublishPackImmediate tests immediate publishing bypasses debounce.
func TestManager_PublishPackImmediate(t *testing.T) {
	cfg := &config.Config{
		Namespace: "test",
		TagPacks: []config.TagPackConfig{
			{
				Name:    "pack1",
				Enabled: true,
				Members: []config.TagPackMember{
					{PLC: "plc1", Tag: "tag1"},
				},
			},
		},
	}

	provider := &mockDataProvider{
		values: map[string]mockTagValue{
			"plc1/tag1": {value: int32(100), typeName: "DINT"},
		},
	}

	m := NewManager(cfg, provider)
	defer m.Stop()

	var publishTimes []time.Time
	var mu sync.Mutex

	m.SetOnPublish(func(info PackPublishInfo) {
		mu.Lock()
		publishTimes = append(publishTimes, time.Now())
		mu.Unlock()
	})

	start := time.Now()

	// Publish immediately
	m.PublishPackImmediate("pack1")

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(publishTimes) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(publishTimes))
	}

	// Should have published immediately (within 100ms), not after debounce (250ms)
	elapsed := publishTimes[0].Sub(start)
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected immediate publish, but took %v", elapsed)
	}
}

// TestManager_GetPackValue tests getting pack values without publishing.
func TestManager_GetPackValue(t *testing.T) {
	cfg := &config.Config{
		Namespace: "test",
		TagPacks: []config.TagPackConfig{
			{
				Name:    "pack1",
				Enabled: true,
				Members: []config.TagPackMember{
					{PLC: "plc1", Tag: "tag1"},
					{PLC: "plc1", Tag: "tag2"},
				},
			},
		},
	}

	provider := &mockDataProvider{
		values: map[string]mockTagValue{
			"plc1/tag1": {value: int32(100), typeName: "DINT"},
			"plc1/tag2": {value: int32(200), typeName: "DINT"},
		},
	}

	m := NewManager(cfg, provider)
	defer m.Stop()

	pv := m.GetPackValue("pack1")

	if pv == nil {
		t.Fatal("expected non-nil pack value")
	}

	if pv.Name != "pack1" {
		t.Errorf("expected name 'pack1', got %q", pv.Name)
	}

	if len(pv.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(pv.Tags))
	}

	// Verify tag values
	if tag1, ok := pv.Tags["plc1.tag1"]; !ok {
		t.Error("missing tag 'plc1.tag1'")
	} else if tag1.Value != int32(100) {
		t.Errorf("expected value 100, got %v", tag1.Value)
	}
}

// TestManager_GetPackValue_WithAlias tests alias handling in GetPackValue.
func TestManager_GetPackValue_WithAlias(t *testing.T) {
	cfg := &config.Config{
		Namespace: "test",
		TagPacks: []config.TagPackConfig{
			{
				Name:    "pack1",
				Enabled: true,
				Members: []config.TagPackMember{
					{PLC: "s7", Tag: "DB1.0"}, // S7 address
				},
			},
		},
	}

	provider := &mockDataProvider{
		values: map[string]mockTagValue{
			"s7/DB1.0": {
				value:    int32(25),
				typeName: "DINT",
				alias:    "sensor_temp", // Provider returns alias
			},
		},
	}

	m := NewManager(cfg, provider)
	defer m.Stop()

	pv := m.GetPackValue("pack1")

	if pv == nil {
		t.Fatal("expected non-nil pack value")
	}

	// Key should use alias
	tag, ok := pv.Tags["s7.sensor_temp"]
	if !ok {
		t.Error("expected key 's7.sensor_temp' (using alias)")
	}

	// MemLoc should have original address
	if tag.MemLoc != "DB1.0" {
		t.Errorf("expected memloc 'DB1.0', got %q", tag.MemLoc)
	}
}

// TestManager_ListPacks tests listing all packs.
func TestManager_ListPacks(t *testing.T) {
	cfg := &config.Config{
		Namespace: "test",
		TagPacks: []config.TagPackConfig{
			{Name: "pack1", Enabled: true, Members: []config.TagPackMember{{PLC: "plc1", Tag: "tag1"}}},
			{Name: "pack2", Enabled: false, Members: []config.TagPackMember{{PLC: "plc1", Tag: "tag2"}}},
			{Name: "pack3", Enabled: true, Members: []config.TagPackMember{{PLC: "plc1", Tag: "tag3"}, {PLC: "plc1", Tag: "tag4"}}},
		},
	}

	m := NewManager(cfg, &mockDataProvider{})
	defer m.Stop()

	packs := m.ListPacks()

	if len(packs) != 3 {
		t.Fatalf("expected 3 packs, got %d", len(packs))
	}

	// Find pack2 and verify it's disabled
	for _, p := range packs {
		if p.Name == "pack2" {
			if p.Enabled {
				t.Error("pack2 should be disabled")
			}
		}
		if p.Name == "pack3" {
			if p.Members != 2 {
				t.Errorf("pack3 should have 2 members, got %d", p.Members)
			}
		}
	}
}

// TestManager_Reload tests configuration reload.
func TestManager_Reload(t *testing.T) {
	cfg := &config.Config{
		Namespace: "test",
		TagPacks: []config.TagPackConfig{
			{Name: "pack1", Enabled: true, Members: []config.TagPackMember{{PLC: "plc1", Tag: "tag1"}}},
		},
	}

	m := NewManager(cfg, &mockDataProvider{})
	defer m.Stop()

	// Verify initial state
	packs := m.ListPacks()
	if len(packs) != 1 {
		t.Fatalf("expected 1 pack initially, got %d", len(packs))
	}

	// Modify config
	cfg.TagPacks = []config.TagPackConfig{
		{Name: "pack1", Enabled: true, Members: []config.TagPackMember{{PLC: "plc1", Tag: "tag1"}}},
		{Name: "pack2", Enabled: true, Members: []config.TagPackMember{{PLC: "plc1", Tag: "tag2"}}},
	}

	// Reload
	m.Reload()

	// Verify new state
	packs = m.ListPacks()
	if len(packs) != 2 {
		t.Errorf("expected 2 packs after reload, got %d", len(packs))
	}
}

// TestPackValue_ValueAccuracy tests that pack values match source values.
func TestPackValue_ValueAccuracy(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		value    interface{}
	}{
		{"int32_max", "DINT", int32(2147483647)},
		{"int32_min", "DINT", int32(-2147483648)},
		{"float64_precise", "LREAL", float64(3.141592653589793)},
		{"bool_true", "BOOL", true},
		{"string_unicode", "STRING", "测试数据"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pv := PackValue{
				Name:      "test",
				Timestamp: time.Now().UTC(),
				Tags: map[string]TagData{
					"plc.tag": {
						Value: tc.value,
						Type:  tc.typeName,
						PLC:   "plc",
					},
				},
			}

			data, err := json.Marshal(pv)
			if err != nil {
				t.Fatalf("marshal error: %v", err)
			}

			var decoded PackValue
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}

			tag := decoded.Tags["plc.tag"]

			// Verify value accuracy
			switch v := tc.value.(type) {
			case int32:
				if tag.Value.(float64) != float64(v) {
					t.Errorf("value mismatch: expected %v, got %v", v, tag.Value)
				}
			case float64:
				if tag.Value.(float64) != v {
					t.Errorf("value mismatch: expected %v, got %v", v, tag.Value)
				}
			case bool:
				if tag.Value.(bool) != v {
					t.Errorf("value mismatch: expected %v, got %v", v, tag.Value)
				}
			case string:
				if tag.Value.(string) != v {
					t.Errorf("value mismatch: expected %q, got %q", v, tag.Value)
				}
			}
		})
	}
}

// Mock implementations for testing

type mockTagValue struct {
	value    interface{}
	typeName string
	alias    string
}

type mockDataProvider struct {
	values   map[string]mockTagValue
	metadata map[string]PLCMetadata
}

func (p *mockDataProvider) GetTagValue(plcName, tagName string) (value interface{}, typeName, alias string, ok bool) {
	key := plcName + "/" + tagName
	if v, exists := p.values[key]; exists {
		return v.value, v.typeName, v.alias, true
	}
	return nil, "", "", false
}

func (p *mockDataProvider) GetPLCMetadata(plcName string) PLCMetadata {
	if p.metadata != nil {
		if m, ok := p.metadata[plcName]; ok {
			return m
		}
	}
	return PLCMetadata{Connected: true}
}
