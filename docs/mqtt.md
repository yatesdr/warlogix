# MQTT Integration

WarLogix publishes tag values and health status to MQTT brokers and supports write-back via MQTT messages.


<img width="902" height="541" alt="image" src="https://github.com/user-attachments/assets/68c2eaeb-7797-46e6-bae9-c633385a8099" />



## Configuration

```yaml
namespace: factory                  # Required: instance namespace

mqtt:
  - name: LocalBroker
    enabled: true
    broker: localhost
    port: 1883
    client_id: warlogix-main
    selector: line1                 # Optional: sub-namespace
    username: user                  # Optional
    password: pass                  # Optional
    use_tls: true                   # Optional
```

The `namespace` is a required top-level setting that identifies this WarLogix instance. The optional `selector` provides additional sub-organization within the namespace.

## Topics

### Tag Values

Published to: `{namespace}[/{selector}]/{plc}/tags/{tag}`

```json
{
  "topic": "factory/line1",
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

Published every 10 seconds to: `{namespace}[/{selector}]/{plc}/health`

```json
{
  "topic": "factory/line1",
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

Send write requests to: `{namespace}[/{selector}]/{plc}/write`

```json
{
  "topic": "factory/line1",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100
}
```

**Requirements:**
- The `topic` field must match the broker's namespace (and selector if configured)
- Tag must be marked as `writable: true` in configuration

### Write Response

Published to: `{namespace}[/{selector}]/{plc}/write/response`

**Success:**
```json
{
  "topic": "factory/line1",
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
  "topic": "factory/line1",
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
namespace: factory

mqtt:
  - name: Production
    enabled: true
    broker: mqtt.production.local
    selector: prod

  - name: Development
    enabled: true
    broker: mqtt.dev.local
    selector: dev
```

All enabled brokers receive the same tag updates. Use different `selector` values to distinguish data streams on different brokers.

## Stress Testing

Use the built-in stress test to benchmark your MQTT broker:

```bash
warlogix --stress-test-republishing
```

This publishes simulated PLC tag data to a test topic (`warlogix-test-stress/+/tags/+`) for 10 seconds.

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `--test-duration` | 10s | Duration of each test |
| `--test-tags` | 100 | Number of simulated tags |
| `--test-plcs` | 50 | Number of simulated PLCs |
| `-y` | false | Skip confirmation prompt |

### Example

```bash
warlogix --stress-test-republishing --test-duration 30s --test-tags 200 -y
```

The test measures throughput (messages per second) and reports any publish failures. MQTT publishes are asynchronous with QoS 1, so the throughput represents the queue rate to the broker.

### Use Cases

- **Baseline performance** - Record expected throughput for your broker
- **Regression testing** - Detect performance changes after updates
- **Capacity planning** - Determine if broker can handle expected tag volume
