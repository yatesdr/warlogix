# Event Triggers

Triggers capture data snapshots when PLC conditions are met and publish to MQTT and/or Kafka with optional acknowledgment. Configure triggers in the Triggers tab or see [Configuration Reference](configuration.md) for YAML options.

## How Triggers Work

1. Monitor a trigger tag for a condition (e.g., `ProductReady == true`)
2. When condition is met (rising edge), capture configured data tags
3. Publish JSON message to configured brokers:
   - **MQTT**: Published with QoS 2 (exactly-once delivery)
   - **Kafka**: Published with configured acknowledgment level
4. Optionally write acknowledgment to PLC (1=success, -1=error)

## Service Selection

Triggers can publish to multiple MQTT brokers and Kafka clusters:

| Value | Behavior |
|-------|----------|
| `all` | Publish to all configured brokers/clusters (default) |
| `none` | Disable publishing to this service |
| `{name}` | Publish only to the named broker/cluster |

## Condition Operators

| Operator | Description |
|----------|-------------|
| `==` | Equal to |
| `!=` | Not equal to |
| `>` | Greater than |
| `<` | Less than |
| `>=` | Greater than or equal |
| `<=` | Less than or equal |

## Message Format

Trigger messages are published to both MQTT and Kafka with the same JSON format. All configured tags and packs are captured atomically and included in a single message:

```json
{
  "trigger": "ProductComplete",
  "timestamp": "2024-01-15T10:30:00.123456789Z",
  "sequence": 42,
  "plc": "MainPLC",
  "metadata": {
    "line": "Line1",
    "station": "Assembly"
  },
  "data": {
    "ProductID": 12345,
    "BatchNumber": "B2024-001",
    "Quantity": 100,
    "Temperature": 72.5,
    "ProductionMetrics": {
      "MainPLC.Counter": { "value": 1234, "type": "DINT", "plc": "MainPLC" },
      "MainPLC.Speed": { "value": 100.5, "type": "REAL", "plc": "MainPLC" },
      "SecondaryPLC.Status": { "value": 1, "type": "INT", "plc": "SecondaryPLC" }
    }
  }
}
```

| Field | Description |
|-------|-------------|
| `trigger` | Trigger name |
| `timestamp` | Nanosecond-precision capture time |
| `sequence` | Incrementing sequence number (resets on restart) |
| `plc` | Source PLC name |
| `metadata` | Static metadata from config |
| `data` | Captured tag values and pack data (packs appear as nested objects) |

### Topics

| Service | Topic Pattern |
|---------|---------------|
| MQTT | `{namespace}[/{selector}]/triggers/{trigger-name}` |
| Kafka | `{namespace}[-{selector}]-triggers` |

MQTT publishes with **QoS 2** (exactly-once delivery) for reliable event capture.

## TagPack Integration

Triggers can include TagPack data in their snapshot. Add packs to a trigger's data tags using the `pack:` prefix (e.g., `pack:ProductionMetrics`). Packs are embedded directly in the trigger message alongside regular tags.

When the trigger fires:
1. All configured tags are read from the PLC
2. All referenced packs have their tag data collected
3. Everything is combined into a single atomic JSON message
4. The message is published to MQTT and/or Kafka

Packs appear in the `data` field as nested objects containing their tag data (see Message Format above).

See [TagPacks](tagpacks.md) for more details on TagPack configuration.

## Acknowledgment

When `ack_tag` is configured, WarLogix writes to the PLC after publishing:

| Value | Meaning |
|-------|---------|
| `1` | Success - message published to Kafka |
| `-1` | Error - capture or publish failed |

The ack tag must be:
- Marked as `writable: true` in the PLC's tag configuration
- A numeric type (INT, DINT, etc.)

**Typical PLC logic:**
1. Set trigger tag (e.g., `ProductReady := TRUE`)
2. Wait for ack tag to become non-zero
3. Read ack value (1=success, -1=error)
4. Reset trigger tag and ack tag for next cycle

## Debouncing

Set `debounce_ms` to prevent rapid re-triggering. This ignores condition matches within the debounce window after the last fire.

## Keyboard Shortcuts

The Triggers tab uses context-sensitive hotkeys based on which pane has focus:

**Trigger list (left pane):**

| Key | Action |
|-----|--------|
| `a` | Add new trigger |
| `x` | Remove selected trigger (with confirmation) |
| `e` | Edit selected trigger |
| `s` | Start/arm trigger |
| `S` | Stop/disarm trigger |
| `F` | Fire trigger (test mode, does not enter cooldown) |

**Data tags list (right pane):**

| Key | Action |
|-----|--------|
| `a` | Add tag or pack to capture list |
| `x` | Remove selected tag (with confirmation) |

## Use Cases

**Production Tracking**: Monitor a "part complete" tag, capture serial number, cycle time, and pass/fail status, publish to Kafka for traceability.

**Alarm Capture**: Monitor alarm word, capture alarm details and process state when alarms occur, use debouncing to prevent spam on rapid alarms.

## Notes

- Triggers fire on **rising edge** only (condition becoming true)
- All data tags are read atomically at trigger time
- Sequence numbers reset when WarLogix restarts
- Failed Kafka publishes are retried per Kafka cluster settings
- Ack writes fail silently if PLC is disconnected
