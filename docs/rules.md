# Rules Engine

Rules are WarLink's unified automation system. A rule monitors one or more PLC tag conditions and executes actions when conditions are met. Rules replace the separate Triggers and Push systems from earlier versions, combining their capabilities into a single, more flexible framework.

## Overview

Each rule consists of:
- **Conditions** — One or more PLC tag conditions combined with AND or OR logic
- **Actions** — Executed on rising edge (conditions become true): publish to MQTT/Kafka, send HTTP webhooks, or write values to PLC tags
- **Cleared actions** (optional) — Executed on falling edge (conditions go back to false)

## Configuration

```yaml
rules:
  - name: ProductComplete
    enabled: true
    conditions:
      - plc: MainPLC
        tag: Program:MainProgram.ProductReady
        operator: "=="
        value: true
    logic_mode: and          # "and" (default) or "or"
    debounce_ms: 100
    cooldown_ms: 5000        # Minimum interval before re-arming
    actions:
      - type: publish
        tag_or_pack: pack:ProductionMetrics
        include_trigger: true
        mqtt_broker: all
        kafka_cluster: ProductionKafka
        kafka_topic: production-events
      - type: writeback
        write_tag: Program:MainProgram.ProductAck
        write_value: 1
    cleared_actions:
      - type: writeback
        write_tag: Program:MainProgram.ProductAck
        write_value: 0
```

See [Configuration Reference](configuration.md#rule-configuration) for the full field reference.

## Conditions

Each condition references a PLC, tag, comparison operator, and value:

```yaml
conditions:
  - plc: MainPLC
    tag: Program:MainProgram.Temperature
    operator: ">"
    value: 100
  - plc: MainPLC
    tag: Program:MainProgram.AlarmBit
    operator: "=="
    value: true
```

### Operators

| Operator | Description |
|----------|-------------|
| `==` | Equal to |
| `!=` | Not equal to |
| `>` | Greater than |
| `<` | Less than |
| `>=` | Greater than or equal |
| `<=` | Less than or equal |

### Logic Mode

| Mode | Behavior |
|------|----------|
| `and` (default) | All conditions must be true to fire |
| `or` | Any single condition being true fires the rule |

### Condition Negation

Add `not: true` to a condition to invert its result:

```yaml
conditions:
  - plc: MainPLC
    tag: RunningFlag
    operator: "=="
    value: true
    not: true        # Fires when RunningFlag is NOT true
```

## Action Types

Rules support three action types. A single rule can have multiple actions of any type, and they execute in parallel when the rule fires.

### Publish

Captures tag or TagPack data and publishes to MQTT and/or Kafka.

```yaml
actions:
  - type: publish
    tag_or_pack: pack:ProductionMetrics    # "pack:Name" for TagPacks, or a tag name
    include_trigger: true                  # Include condition tag values in message
    mqtt_broker: all                       # "all", "none", or a specific broker name
    mqtt_topic: rules/ProductComplete      # Custom MQTT sub-topic (default: rules/{rule-name})
    kafka_cluster: all                     # "all", "none", or a specific cluster name
    kafka_topic: production-events         # Custom Kafka topic suffix (default: rules)
```

**Service selection:**

| Value | Behavior |
|-------|----------|
| `all` | Publish to all configured brokers/clusters (default) |
| `none` | Disable publishing to this service |
| `{name}` | Publish only to the named broker/cluster |

MQTT publishes with **QoS 2** (exactly-once delivery).

### Webhook

Sends an HTTP request to an external endpoint with body template support.

```yaml
actions:
  - type: webhook
    name: SlackAlert                       # Label for logging
    url: https://hooks.slack.com/services/T00/B00/xxxx
    method: POST                           # GET, POST, PUT, PATCH (default: POST)
    content_type: application/json
    body: '{"text": "Alarm on MainPLC, temp: #MainPLC.Temperature"}'
    headers:
      X-Source: warlink
    auth:
      type: bearer
      token: "your-api-token"
    timeout: 30s
```

**Body templates** — Use `#PLCName.tagName` references in the body to include live tag values. References are resolved at send time. If a tag cannot be read, the reference is left as-is.

**Authentication types:**

| Type | Fields | Description |
|------|--------|-------------|
| `bearer` | `token` | Sends `Authorization: Bearer <token>` |
| `jwt` | `token` | Same as bearer (alias) |
| `basic` | `username`, `password` | HTTP Basic Authentication |
| `custom_header` | `header_name`, `header_value` | Custom header (e.g., `X-API-Key`) |

### Writeback

Writes a value to a PLC tag when the rule fires or clears.

```yaml
actions:
  - type: writeback
    write_plc: MainPLC                     # PLC name (defaults to first condition's PLC)
    write_tag: Program:MainProgram.AckFlag
    write_value: 1                         # Value to write (any type)
```

The write tag must be marked as `writable: true` in the PLC's tag configuration.

## State Machine

```
┌──────────┐   conditions    ┌──────────┐   actions done   ┌─────────────┐
│  Armed   │────────────────▶│  Firing  │────────────────▶│WaitingClear │
└──────────┘  rising edge    └──────────┘                  └──────┬──────┘
     ▲                                                            │
     │                                                            │ conditions
     │                            ┌──────────┐                    │ all false
     └────────────────────────────│ Cooldown │◀───────────────────┘
              interval elapsed   └──────────┘   (cleared actions run here)
```

| State | Description |
|-------|-------------|
| Disabled | Rule is stopped |
| Armed | Monitoring conditions for rising edge |
| Firing | Executing actions |
| Waiting Clear | Actions complete, waiting for conditions to go false |
| Cooldown | Conditions cleared, waiting minimum interval before re-arming |
| Error | Action execution failed |

### Rising and Falling Edge

- **Rising edge** (false → true): The rule's `actions` fire
- **Falling edge** (true → false): The rule's `cleared_actions` fire (if configured)

This enables patterns like setting an acknowledgment flag when a condition is met and clearing it when the condition resets.

## Message Format

Publish actions produce JSON messages sent to MQTT and Kafka:

```json
{
  "rule": "ProductComplete",
  "timestamp": "2024-01-15T10:30:00.123456789Z",
  "sequence": 42,
  "plc": "MainPLC",
  "trigger": {
    "MainPLC.ProductReady": true
  },
  "data": {
    "ProductionMetrics": {
      "MainPLC.Counter": { "value": 1234, "type": "DINT", "plc": "MainPLC" },
      "MainPLC.Speed": { "value": 100.5, "type": "REAL", "plc": "MainPLC" }
    }
  }
}
```

| Field | Description |
|-------|-------------|
| `rule` | Rule name |
| `timestamp` | Nanosecond-precision capture time |
| `sequence` | Incrementing sequence number (resets on restart) |
| `plc` | First condition's PLC name |
| `trigger` | Condition tag values (when `include_trigger: true`) |
| `data` | Captured tag values and pack data |

### Topics

| Service | Topic Pattern |
|---------|---------------|
| MQTT | `{namespace}/rules/{rule-name}` (or custom `mqtt_topic`) |
| Kafka | `{namespace}-rules` (or custom `kafka_topic`) |

## TUI Keyboard Shortcuts

**Rule list (left pane):**

| Key | Action |
|-----|--------|
| `a` | Add new rule |
| `x` | Remove selected rule |
| `e` | Edit selected rule |
| `Space` | Toggle enabled/disabled |
| `F` | Test fire (bypasses conditions and cooldown) |
| `Tab` | Switch to conditions pane |

## Debouncing

Set `debounce_ms` to prevent rapid re-firing. Condition matches within the debounce window after the last fire are ignored.

## Cooldown

Set `cooldown_ms` to enforce a minimum interval between fires. After conditions clear, the rule waits for the cooldown period before re-arming. During cooldown, the rule will not fire even if conditions become true again.

## Test Firing

Test fire executes all actions immediately, bypassing conditions and cooldown. This is useful for verifying endpoint connectivity, message format, and writeback behavior. Test fires increment the fire counter but do not enter cooldown state.

## Migration from Triggers and Push

Rules replace both Triggers and Push from v0.2.7 and earlier. To migrate:

**Triggers** → Create a rule with:
- The trigger's condition as the rule condition
- A `publish` action for the MQTT/Kafka publishing
- A `writeback` action for the ack tag (if used)

**Push webhooks** → Create a rule with:
- The push conditions as rule conditions (set `logic_mode: or` if using OR logic)
- A `webhook` action with the URL, method, body, and auth settings

## Examples

### Production Tracking with Acknowledgment

```yaml
rules:
  - name: PartComplete
    enabled: true
    conditions:
      - plc: LinePLC
        tag: Program:Main.PartReady
        operator: "=="
        value: true
    debounce_ms: 100
    cooldown_ms: 1000
    actions:
      - type: publish
        tag_or_pack: pack:PartData
        include_trigger: true
        kafka_cluster: all
        kafka_topic: production-events
      - type: writeback
        write_tag: Program:Main.PartAck
        write_value: 1
    cleared_actions:
      - type: writeback
        write_tag: Program:Main.PartAck
        write_value: 0
```

### Slack Alarm Notification

```yaml
rules:
  - name: HighTempAlarm
    enabled: true
    conditions:
      - plc: MainPLC
        tag: Temperature
        operator: ">"
        value: 100
    cooldown_ms: 1800000   # 30 minutes
    actions:
      - type: webhook
        name: SlackAlert
        url: https://hooks.slack.com/services/T00/B00/xxxx
        body: '{"text": "High temperature alarm: #MainPLC.Temperature degrees"}'
```

### Multi-Condition AND Rule

```yaml
rules:
  - name: QualityCheck
    enabled: true
    logic_mode: and
    conditions:
      - plc: LinePLC
        tag: BatchComplete
        operator: "=="
        value: true
      - plc: LinePLC
        tag: QualityScore
        operator: ">="
        value: 95
    actions:
      - type: publish
        tag_or_pack: pack:BatchResults
        mqtt_broker: all
      - type: webhook
        url: https://mes.example.com/api/batch-pass
        body: '{"batch": #LinePLC.BatchID, "score": #LinePLC.QualityScore}'
```

## Notes

- Rules fire on **rising edge** only; cleared actions fire on **falling edge**
- All actions within a rule execute in parallel
- Sequence numbers reset when WarLink restarts
- Failed actions are logged but do not block other actions
- Writeback failures are logged silently if the PLC is disconnected
- The condition poll interval is 100ms
