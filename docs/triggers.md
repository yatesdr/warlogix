# Event Triggers

Triggers capture data snapshots when PLC conditions are met and publish to Kafka with optional acknowledgment.

## How Triggers Work

1. Monitor a trigger tag for a condition (e.g., `ProductReady == true`)
2. When condition is met (rising edge), capture configured data tags
3. Publish JSON message to Kafka with timestamp and sequence number
4. Optionally write acknowledgment to PLC (1=success, -1=error)

## Configuration

```yaml
triggers:
  - name: ProductComplete
    enabled: true
    plc: MainPLC
    trigger_tag: Program:MainProgram.ProductReady
    condition:
      operator: "=="
      value: true
    ack_tag: Program:MainProgram.ProductAck    # Optional
    debounce_ms: 100                            # Optional
    tags:
      - ProductID
      - BatchNumber
      - Quantity
      - Temperature
    kafka_cluster: LocalKafka
    topic: production-events
    metadata:                                   # Optional static data
      line: Line1
      station: Assembly
```

## Condition Operators

| Operator | Description |
|----------|-------------|
| `==` | Equal to |
| `!=` | Not equal to |
| `>` | Greater than |
| `<` | Less than |
| `>=` | Greater than or equal |
| `<=` | Less than or equal |

## Kafka Message Format

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
    "Temperature": 72.5
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
| `data` | Captured tag values |

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

Set `debounce_ms` to prevent rapid re-triggering:

```yaml
debounce_ms: 100    # Ignore triggers within 100ms of last fire
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `a` | Add new trigger |
| `e` | Edit selected trigger |
| `r` | Remove selected trigger |
| `t` | Add data tag to trigger |
| `x` | Remove data tag from trigger |
| `s` | Start/enable trigger |
| `S` | Stop/disable trigger |
| `T` | Test trigger (fire manually) |

## Example: Production Tracking

```yaml
triggers:
  - name: PartComplete
    enabled: true
    plc: MainPLC
    trigger_tag: Program:Production.PartDone
    condition: { operator: "==", value: true }
    ack_tag: Program:Production.PartAck
    debounce_ms: 50
    tags:
      - Program:Production.PartNumber
      - Program:Production.SerialNumber
      - Program:Production.CycleTime
      - Program:Production.PassFail
    kafka_cluster: ProductionKafka
    topic: parts-produced
    metadata:
      cell: Cell-A1
      shift: auto    # Could be updated dynamically
```

## Example: Alarm Capture

```yaml
triggers:
  - name: CriticalAlarm
    enabled: true
    plc: MainPLC
    trigger_tag: Alarms.Critical
    condition: { operator: ">", value: 0 }
    debounce_ms: 1000    # Don't spam on rapid alarms
    tags:
      - Alarms.ActiveCode
      - Alarms.Description
      - Process.CurrentState
      - Process.LastOperation
    kafka_cluster: AlertsKafka
    topic: critical-alarms
```

## Notes

- Triggers fire on **rising edge** only (condition becoming true)
- All data tags are read atomically at trigger time
- Sequence numbers reset when WarLogix restarts
- Failed Kafka publishes are retried per Kafka cluster settings
- Ack writes fail silently if PLC is disconnected
