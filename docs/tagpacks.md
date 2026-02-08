# TagPacks

TagPacks group tags from multiple PLCs and publish them atomically as a single JSON message when any non-ignored member changes.

## Use Cases

- **Cross-PLC Snapshots**: Capture related data from multiple PLCs at the same instant
- **Atomic Publishing**: Ensure all tag values in a group are from the same moment
- **Reduced Message Volume**: Publish one message instead of many individual tag updates
- **Debounced Updates**: Aggregate rapid changes with 250ms debouncing

## How TagPacks Work

1. Define a pack with members from any connected PLCs
2. Optionally mark volatile members as "ignored" (they won't trigger publishes)
3. When any non-ignored member changes, a 250ms debounce timer starts
4. After debounce, all member values are collected and published atomically
5. Published JSON includes tag values plus PLC metadata (connection status, IP, model)

## Configuration

```yaml
tag_packs:
  - name: ProductionMetrics
    enabled: true
    topic: packs/production
    mqtt_enabled: true
    kafka_enabled: true
    valkey_enabled: false
    members:
      - plc: MainPLC
        tag: ProductCount
        # ignore_changes: false   # Default: changes trigger publish
      - plc: MainPLC
        tag: Temperature
      - plc: SecondaryPLC
        tag: ConveyorSpeed
        ignore_changes: true      # Changes to this tag don't trigger publish
      - plc: SecondaryPLC
        tag: AlarmStatus
        ignore_changes: true      # Included in pack but ignored for triggering
```

### Configuration Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier for the pack |
| `enabled` | bool | No | Enable/disable the pack (default: false) |
| `topic` | string | Yes | Topic/channel name for all brokers |
| `mqtt_enabled` | bool | No | Publish to MQTT brokers |
| `kafka_enabled` | bool | No | Publish to Kafka clusters |
| `valkey_enabled` | bool | No | Publish to Valkey/Redis |
| `members` | list | Yes | Tags included in the pack |

### Member Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `plc` | string | Yes | PLC name (must exist in config) |
| `tag` | string | Yes | Tag name to include |
| `ignore_changes` | bool | No | If true, changes to this tag don't trigger publish (default: false) |

## Published JSON Format

The pack is published with a flat `plc.tag` key structure for easy access and to prevent naming collisions when multiple PLCs have tags with the same name:

```json
{
  "name": "ProductionMetrics",
  "timestamp": "2024-01-15T10:30:00.123Z",
  "tags": {
    "MainPLC.TotalProduced": {
      "value": 1234,
      "type": "DINT",
      "plc": "MainPLC",
      "offset": "ProductCount"
    },
    "MainPLC.Temperature": {
      "value": 72.5,
      "type": "REAL",
      "plc": "MainPLC"
    },
    "SecondaryPLC.ConveyorSpeed": {
      "value": 100,
      "type": "INT",
      "plc": "SecondaryPLC"
    },
    "SecondaryPLC.AlarmStatus": {
      "value": 0,
      "type": "DINT",
      "plc": "SecondaryPLC"
    }
  }
}
```

### Key Structure

- **Map key format**: `plc.tag` (e.g., `MainPLC.Counter`, `s7.test_wstring`)
- **Alias handling**: When a tag has an alias, the alias is used in the key and the original tag name/address is stored in the `offset` field
- **PLC field**: Each tag entry includes the PLC name for easy filtering by consumer applications

### Tag Entry Fields

| Field | Description |
|-------|-------------|
| `value` | Current tag value |
| `type` | PLC data type (e.g., DINT, REAL, STRING) |
| `plc` | PLC name (for filtering when consuming) |
| `offset` | Original tag name/address when alias is used (omitted otherwise) |

### PLC Metadata (Error Handling)

If a PLC has connection issues, a `plcs` field is included with error details:

```json
{
  "name": "ProductionMetrics",
  "timestamp": "2024-01-15T10:30:00.123Z",
  "tags": {
    "MainPLC.Temperature": {
      "value": 72.5,
      "type": "REAL",
      "plc": "MainPLC"
    },
    "SecondaryPLC.ConveyorSpeed": {
      "value": null,
      "type": "",
      "plc": "SecondaryPLC"
    }
  },
  "plcs": {
    "SecondaryPLC": {
      "address": "192.168.1.101",
      "family": "logix",
      "connected": false,
      "error": "connection timeout"
    }
  }
}
```

### Top-Level JSON Fields

| Field | Description |
|-------|-------------|
| `name` | TagPack name |
| `timestamp` | UTC timestamp when pack was collected |
| `tags` | Flat map with `plc.tag` keys → tag data |
| `plcs` | PLC metadata (only included when there are connection errors) |

## TUI Keyboard Shortcuts

Navigate to the **TagPacks** tab to manage packs.

| Key | Action |
|-----|--------|
| `c` | Create new pack |
| `a` | Add tag to selected pack |
| `d` | Delete selected pack or member |
| `Space` | Toggle pack enabled/disabled (publishes immediately when enabled) |
| `i` | Toggle member ignore (ignored members don't trigger publish) |
| `e` | Edit pack settings (topic, brokers) |
| `r` | Rename pack |
| `Tab` | Switch focus between pack list and member list |

**Note:** By default, changes to any member trigger a pack publish. Use `i` to mark members as "ignored" - these tags are still included in the pack data but their changes won't trigger a publish (useful for volatile data like counters or timestamps).

## Finding Tags with the Tag Picker

When adding tags to a pack, use the filter to quickly find tags across all PLCs.

### Filter Syntax

| Filter | Matches |
|--------|---------|
| `temp` | Any tag with "temp" in PLC name OR tag name |
| `main:` | All tags from PLCs containing "main" |
| `:count` | Tags containing "count" from any PLC |
| `logix:prod` | Tags containing "prod" from PLCs containing "logix" |

### Examples

```
Filter: main:temp
→ MainPLC:Temperature
→ MainPLC:TempSetpoint

Filter: :alarm
→ MainPLC:AlarmCode
→ SecondaryPLC:AlarmStatus
→ BackupPLC:AlarmHistory

Filter: plc1:
→ PLC1:Counter
→ PLC1:Speed
→ PLC1:Status
```

**Tip:** Type the PLC name followed by `:` to see all its tags, then continue typing to filter by tag name.

## REST API

TagPacks are available via REST similar to PLCs.

### List All TagPacks

```
GET /tagpack
```

Response:
```json
[
  {"name": "ProductionMetrics", "enabled": true, "topic": "packs/production", "members": 4, "url": "/tagpack/ProductionMetrics"},
  {"name": "Alarm Pack", "enabled": false, "topic": "packs/alarms", "members": 8, "url": "/tagpack/Alarm%20Pack"}
]
```

Note: The `url` field contains the URL-encoded path to access the pack details.

### Get TagPack Details

```
GET /tagpack/{name}
```

Response: Full PackValue JSON with current tag values (same flat `plc.tag` key format as published messages):

```json
{
  "name": "ProductionMetrics",
  "timestamp": "2024-01-15T10:30:00.123Z",
  "tags": {
    "MainPLC.TotalProduced": {
      "value": 1234,
      "type": "DINT",
      "plc": "MainPLC",
      "offset": "ProductCount"
    },
    "MainPLC.Temperature": {
      "value": 72.5,
      "type": "REAL",
      "plc": "MainPLC"
    }
  }
}
```

## Integration with Triggers

Triggers can publish a TagPack immediately when they fire, bypassing the normal 250ms debounce.

```yaml
triggers:
  - name: ProductComplete
    plc: MainPLC
    trigger_tag: ProductReady
    condition: { operator: "==", value: true }
    publish_pack: ProductionMetrics    # Publish this pack when trigger fires
    kafka_cluster: LocalKafka
    topic: events
```

When the trigger fires:
1. Normal trigger data is captured and published to Kafka
2. The specified TagPack is published immediately to its configured brokers
3. Pack debounce timer is reset (won't double-publish)

## Debouncing

TagPacks use a 250ms debounce to aggregate rapid changes:

1. First non-ignored tag change starts a 250ms timer
2. Additional changes during this window are aggregated (values still update)
3. After 250ms, all current member values are collected and published
4. Timer resets, ready for next change

This prevents message floods when multiple tags change in quick succession.

## Best Practices

### Grouping Strategy

- **By Function**: Group tags that represent a logical unit (e.g., all motor data)
- **By Timing**: Group tags that need to be captured together
- **By Consumer**: Group tags that a specific downstream system needs

### Ignore Selection

- Mark volatile tags (counters, timestamps, timers) as ignored to reduce noise
- Keep important status tags as non-ignored for responsive updates
- Consider ignoring all but one tag if you want controlled publish timing

### Topic Naming

```yaml
topic: packs/production     # Functional grouping
topic: packs/line1/metrics  # Hierarchical
topic: machine/status       # Flat naming
```

### Performance

- Each pack publish reads all member tags atomically
- Large packs (50+ tags) may increase latency slightly
- Cross-PLC packs require reads from multiple connections
- Debouncing reduces publish frequency for rapidly changing data

## Example: Multi-Line Production

```yaml
tag_packs:
  - name: Line1Status
    enabled: true
    topic: factory/line1/status
    mqtt_enabled: true
    kafka_enabled: true
    valkey_enabled: true
    members:
      - { plc: Line1_PLC, tag: RunningState }
      - { plc: Line1_PLC, tag: PartsProduced }
      - { plc: Line1_PLC, tag: CycleTime, ignore_changes: true }
      - { plc: Line1_Robot, tag: Position, ignore_changes: true }
      - { plc: Line1_Robot, tag: GripperState, ignore_changes: true }

  - name: Line2Status
    enabled: true
    topic: factory/line2/status
    mqtt_enabled: true
    kafka_enabled: true
    valkey_enabled: true
    members:
      - { plc: Line2_PLC, tag: RunningState }
      - { plc: Line2_PLC, tag: PartsProduced }
      - { plc: Line2_PLC, tag: CycleTime, ignore_changes: true }
      - { plc: Line2_Vision, tag: InspectionResult }
```

## Example: Alarm Aggregation

```yaml
tag_packs:
  - name: AllAlarms
    enabled: true
    topic: alarms/all
    mqtt_enabled: true
    kafka_enabled: false
    valkey_enabled: true
    members:
      - { plc: MainPLC, tag: AlarmWord1 }
      - { plc: MainPLC, tag: AlarmWord2 }
      - { plc: SafetyPLC, tag: EStopStatus }
      - { plc: SafetyPLC, tag: GuardStatus }
      - { plc: HVAC_PLC, tag: TempAlarm }
      - { plc: HVAC_PLC, tag: PressureAlarm }
```
