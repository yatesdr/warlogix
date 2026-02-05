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
