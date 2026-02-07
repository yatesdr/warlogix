# MQTT Integration

WarLogix publishes tag values and health status to MQTT brokers and supports write-back via MQTT messages.

## Configuration

```yaml
mqtt:
  - name: LocalBroker
    enabled: true
    broker: localhost
    port: 1883
    client_id: warlogix-main
    root_topic: factory
    username: user          # Optional
    password: pass          # Optional
    use_tls: true           # Optional
```

## Topics

### Tag Values

Published to: `{root_topic}/{plc}/tags/{tag}`

```json
{
  "topic": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 42,
  "type": "DINT",
  "writable": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

Tags are published on change. Arrays are published as JSON arrays.

### Health Status

Published every 10 seconds to: `{root_topic}/{plc}/health`

```json
{
  "topic": "factory",
  "plc": "MainPLC",
  "online": true,
  "status": "connected",
  "error": "",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

**Status values:** `connected`, `connecting`, `disconnected`, `disabled`, `error`

Health publishing can be disabled per-PLC with `health_check_enabled: false`.

## Write Requests

Send write requests to: `{root_topic}/{plc}/write`

```json
{
  "topic": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100
}
```

**Requirements:**
- The `topic` field must match the broker's `root_topic`
- Tag must be marked as `writable: true` in configuration

### Write Response

Published to: `{root_topic}/{plc}/write/response`

**Success:**
```json
{
  "topic": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100,
  "success": true,
  "timestamp": "2024-01-15T10:30:05Z"
}
```

**Error:**
```json
{
  "topic": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100,
  "success": false,
  "error": "tag is not writable",
  "timestamp": "2024-01-15T10:30:05Z"
}
```

## TLS Configuration

Enable TLS by setting `use_tls: true`. The system CA certificates are used for verification.

## Multiple Brokers

Configure multiple brokers for redundancy or different purposes:

```yaml
mqtt:
  - name: Production
    enabled: true
    broker: mqtt.production.local
    root_topic: factory/prod

  - name: Development
    enabled: true
    broker: mqtt.dev.local
    root_topic: factory/dev
```

All enabled brokers receive the same tag updates.

## Stress Testing

Use the built-in stress test to benchmark your MQTT broker:

```bash
warlogix --test-brokers
```

This publishes simulated PLC tag data to a test topic (`warlogix-test-stress/+/tags/+`) for 10 seconds.

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `--test-duration` | 10s | Duration of each test |
| `--test-tags` | 100 | Number of simulated tags |
| `--test-plcs` | 5 | Number of simulated PLCs |
| `-y` | false | Skip confirmation prompt |

### Example

```bash
warlogix --test-brokers --test-duration 30s --test-tags 200 -y
```

The test measures throughput (messages per second) and reports any publish failures. MQTT publishes are asynchronous with QoS 1, so the throughput represents the queue rate to the broker.

### Use Cases

- **Baseline performance** - Record expected throughput for your broker
- **Regression testing** - Detect performance changes after updates
- **Capacity planning** - Determine if broker can handle expected tag volume
