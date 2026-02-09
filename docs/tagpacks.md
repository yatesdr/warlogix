# TagPacks

TagPacks group tags from multiple PLCs and publish them atomically as a single JSON message when any non-ignored member changes. Configure packs in the TagPacks tab or see [Configuration Reference](configuration.md) for YAML options.

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

## Topic/Key Naming

TagPack topics and keys are automatically derived from the `namespace` and `selector` settings:

| Service | Topic/Key | Message Key | Example |
|---------|-----------|-------------|---------|
| **MQTT** | `{namespace}[/{selector}]/packs/{packname}` | - | `factory/line1/packs/ProductionMetrics` |
| **Kafka** | `{namespace}[-{selector}]` (same as tags) | `pack:{packname}` | Topic: `factory-line1`, Key: `pack:ProductionMetrics` |
| **Valkey** | `{namespace}[:{selector}]:packs:{packname}` | - | `factory:line1:packs:ProductionMetrics` |

## Storage and Delivery

TagPacks are stored/published consistently with regular tags:

| Service | Behavior |
|---------|----------|
| **MQTT** | Published with `retained: true` - new subscribers receive last value |
| **Kafka** | Published to topic - retained per Kafka topic retention policy |
| **Valkey** | Stored as key AND published to channel - persists and notifies subscribers |

This means TagPacks appear in Redis browsers alongside regular tags, and MQTT clients connecting later will receive the last published pack value.

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

**Pack list (left pane):**

| Key | Action |
|-----|--------|
| `a` | Add new pack |
| `x` | Remove selected pack |
| `Space` | Toggle pack enabled/disabled |
| `e` | Edit pack settings |
| `Tab` | Switch focus to member list |

**Member list (right pane):**

| Key | Action |
|-----|--------|
| `a` | Add tag to pack |
| `x` | Remove selected member |
| `i` | Toggle ignore (ignored members don't trigger publish) |
| `E` | Enable tag in Browser if not already enabled |

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
  {"name": "ProductionMetrics", "enabled": true, "members": 4, "url": "/tagpack/ProductionMetrics"},
  {"name": "Alarm Pack", "enabled": false, "members": 8, "url": "/tagpack/Alarm%20Pack"}
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

Triggers can include TagPack data in their snapshot by adding `pack:PackName` to the trigger's data tags list. When the trigger fires, pack data is embedded directly in the trigger message as a nested object. See [Triggers](triggers.md) for details.

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

### Pack Naming

Use descriptive pack names since they become part of the topic/key:

```yaml
name: ProductionMetrics    # → factory/line1/packs/ProductionMetrics (MQTT with selector)
name: Line1Status          # → factory:line1:packs:Line1Status (Valkey with selector)
name: AlarmSummary         # → Topic: factory-line1, Key: pack:AlarmSummary (Kafka)
```

### Performance

- Each pack publish reads all member tags atomically
- Large packs (50+ tags) may increase latency slightly
- Cross-PLC packs require reads from multiple connections
- Debouncing reduces publish frequency for rapidly changing data

## Use Case Examples

**Multi-Line Production**: Create a pack per production line with running state, parts count, and cycle time. Mark volatile values (cycle time, robot position) as ignored to reduce message volume.

**Alarm Aggregation**: Combine alarm words from multiple PLCs into a single pack for unified alarm monitoring.
