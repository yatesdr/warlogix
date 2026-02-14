# Push Webhooks

Push webhooks monitor PLC tag conditions and send HTTP requests to external endpoints when conditions are met. Use pushes to integrate WarLink with alerting systems, third-party APIs, or any HTTP-based workflow.

## Overview

A push target watches one or more PLC tag conditions and fires an HTTP request on a rising edge (when a condition transitions from false to true). Multiple conditions use OR logic — any single condition can trigger the push. After firing, the push waits for all triggering conditions to clear before re-arming.

### Push vs Triggers

| Feature | Push | Trigger |
|---------|------|---------|
| Output | HTTP request to external URL | MQTT (QoS 2) and/or Kafka message |
| Conditions | Multiple conditions (OR logic) | Single condition per trigger |
| Data payload | Body template with tag references | Captures a set of data tags |
| Write-back | None | Optional ack tag |
| Use case | Webhooks, external API calls, alerts | Event-driven data capture |

## Configuration

```yaml
pushes:
  - name: AlarmWebhook
    enabled: true
    conditions:
      - plc: MainPLC
        tag: Program:MainProgram.AlarmActive
        operator: "=="
        value: true
    url: https://api.example.com/alarm
    method: POST
    content_type: application/json
    body: '{"alarm": true, "temp": #MainPLC.Temperature, "counter": #MainPLC.Counter}'
    auth:
      type: bearer
      token: "your-api-token"
    cooldown_min: 15m
    timeout: 30s
```

See [Configuration Reference](configuration.md#push-configuration) for the full field reference.

## Conditions

Each condition references a PLC, tag, operator, and value:

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

Supported operators: `==`, `!=`, `>`, `<`, `>=`, `<=`

Conditions use **OR logic** — the push fires when any single condition has a rising edge (transitions from not-met to met). After firing, the push waits for all conditions that triggered it to go false before re-arming.

### Per-Condition Cooldown

By default, cooldown is global — all conditions must clear and the cooldown interval must elapse before re-arming. Set `cooldown_per_condition: true` to track cooldown independently per condition, allowing other conditions to fire while one is still in cooldown.

## Body Templates

The body field supports `#PLCName.tagName` references that are replaced with live tag values when the push fires:

```json
{"temperature": #MainPLC.Temperature, "counter": #MainPLC.Counter}
```

- References use the format `#PLCName.tagPath` (e.g., `#PLC1.Program:MainProgram.Counter`)
- If a tag cannot be read, the reference is left as-is in the body
- References are resolved at send time, reflecting the current tag values

## Authentication

| Type | Fields | Description |
|------|--------|-------------|
| `bearer` | `token` | Sends `Authorization: Bearer <token>` |
| `jwt` | `token` | Same as bearer (alias) |
| `basic` | `username`, `password` | HTTP Basic Authentication |
| `custom_header` | `header_name`, `header_value` | Custom header (e.g., `X-API-Key`) |

```yaml
# Bearer token
auth:
  type: bearer
  token: "your-api-token"

# Basic auth
auth:
  type: basic
  username: "user"
  password: "pass"

# Custom header
auth:
  type: custom_header
  header_name: "X-API-Key"
  header_value: "your-key"
```

## Custom Headers

Add arbitrary headers to the request:

```yaml
headers:
  X-Source: "warlink"
  X-Site: "factory-1"
```

## State Machine

```
┌──────────┐   condition    ┌──────────┐   HTTP sent   ┌─────────────┐
│  Armed   │───────────────▶│  Firing  │──────────────▶│WaitingClear │
└──────────┘  rising edge   └──────────┘               └──────┬──────┘
     ▲                                                        │
     │                                                        │ conditions
     │                            ┌──────────┐                │ all false
     └────────────────────────────│ Cooldown │◀───────────────┘
              interval elapsed   └──────────┘
```

| State | Description |
|-------|-------------|
| Disabled | Push is stopped |
| Armed | Monitoring conditions for rising edge |
| Firing | Sending HTTP request |
| Waiting Clear | Request sent, waiting for triggering conditions to go false |
| Cooldown | Conditions cleared, waiting minimum interval before re-arming |
| Error | HTTP request failed (automatically transitions to Waiting Clear) |

## TUI Keyboard Shortcuts

**Push list (left pane):**

| Key | Action |
|-----|--------|
| `a` | Add new push |
| `x` | Remove selected push |
| `e` | Edit selected push |
| `Space` | Toggle enabled/disabled |
| `F` | Test fire (bypasses conditions and cooldown) |
| `Tab` | Switch to conditions pane |

## Test Firing

Test fire sends the HTTP request immediately, bypassing all conditions and cooldown. This is useful for verifying endpoint connectivity and request format. Test fires increment the send counter but do not enter cooldown state.

## Examples

### Slack Notification

```yaml
pushes:
  - name: SlackAlert
    enabled: true
    conditions:
      - plc: MainPLC
        tag: AlarmActive
        operator: "=="
        value: true
    url: https://hooks.slack.com/services/T00/B00/xxxx
    method: POST
    body: '{"text": "Alarm active on MainPLC, temperature: #MainPLC.Temperature"}'
    cooldown_min: 30m
```

### REST API Integration

```yaml
pushes:
  - name: MESUpdate
    enabled: true
    conditions:
      - plc: LinePLC
        tag: Program:Main.BatchComplete
        operator: "=="
        value: true
    url: https://mes.example.com/api/batch
    method: POST
    headers:
      X-API-Key: "mes-integration-key"
    body: '{"batch_id": #LinePLC.BatchID, "quantity": #LinePLC.Quantity, "result": #LinePLC.QualityResult}'
    cooldown_min: 5s
```
