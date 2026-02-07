# Valkey/Redis Integration

WarLogix stores tag values in Valkey/Redis with optional Pub/Sub notifications and write-back queue support.

## Configuration

```yaml
valkey:
  - name: LocalValkey
    enabled: true
    address: localhost:6379
    database: 0
    factory: factory
    password: secret            # Optional
    use_tls: true               # Optional
    key_ttl: 60s                # Optional key expiration
    publish_changes: true       # Enable Pub/Sub
    enable_writeback: true      # Enable write-back queue
```

## Key Storage

### Tag Keys

Pattern: `{factory}:{plc}:tags:{tag}`

```json
{
  "factory": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 42,
  "type": "DINT",
  "writable": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Health Keys

Pattern: `{factory}:{plc}:health`

Updated every 10 seconds:

```json
{
  "factory": "factory",
  "plc": "MainPLC",
  "online": true,
  "status": "connected",
  "error": "",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

Health publishing can be disabled per-PLC with `health_check_enabled: false`.

### Key TTL

Set `key_ttl` to automatically expire keys. Useful for detecting stale data:

```yaml
key_ttl: 60s    # Keys expire after 60 seconds without update
```

## Pub/Sub Channels

When `publish_changes: true`, changes are published to:

| Channel | Description |
|---------|-------------|
| `{factory}:{plc}:changes` | Changes for specific PLC |
| `{factory}:_all:changes` | All changes across all PLCs |

**Subscribe example:**
```bash
redis-cli SUBSCRIBE factory:MainPLC:changes factory:_all:changes
```

## Write-Back Queue

When `enable_writeback: true`, write requests can be sent via a Redis LIST.

### Queue Key

`{factory}:writes`

### Send Write Request

```bash
redis-cli RPUSH factory:writes '{"factory":"factory","plc":"MainPLC","tag":"Counter","value":100}'
```

### Request Format

```json
{
  "factory": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100
}
```

**Requirements:**
- The `factory` field must match the server's `factory` configuration
- Tag must be marked as `writable: true`

### Response Channel

Subscribe to: `{factory}:write:responses`

```bash
redis-cli SUBSCRIBE factory:write:responses
```

**Success:**
```json
{
  "factory": "factory",
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
  "factory": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100,
  "success": false,
  "error": "tag is not writable",
  "timestamp": "2024-01-15T10:30:05Z"
}
```

## Multiple Servers

Configure multiple Valkey/Redis servers:

```yaml
valkey:
  - name: Primary
    enabled: true
    address: redis-primary:6379
    factory: factory

  - name: Replica
    enabled: true
    address: redis-replica:6379
    factory: factory
```

All enabled servers receive the same updates.

## CLI Examples

**Get tag value:**
```bash
redis-cli GET factory:MainPLC:tags:Counter
```

**Get health:**
```bash
redis-cli GET factory:MainPLC:health
```

**List all tags for a PLC:**
```bash
redis-cli KEYS "factory:MainPLC:tags:*"
```

**Write a value:**
```bash
redis-cli RPUSH factory:writes '{"factory":"factory","plc":"MainPLC","tag":"Counter","value":100}'
```

## Stress Testing

Use the built-in stress test to benchmark your Valkey/Redis server:

```bash
warlogix --stress-test-republishing
```

This runs a 10-second stress test against all enabled Valkey servers, writing simulated PLC tag data to test keys (`warlogix-test-stress:*`).

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `--test-duration` | 10s | Duration of each test |
| `--test-tags` | 100 | Number of simulated tags |
| `--test-plcs` | 50 | Number of simulated PLCs |
| `-y` | false | Skip confirmation prompt |

### Example Output

```
  Valkey/local:
    Address:    localhost:6379
    Duration:   10.001s
    Messages:   45123 sent, 0 errors
    Throughput: 4512.1 msg/s
    Latency:
      avg: 180µs, p50: 150µs, p95: 320µs, p99: 890µs, max: 2.1ms
```

The test measures:
- **Throughput** - SET operations per second
- **Latency** - Per-operation latency (avg, p50, p95, p99, max)
- **Errors** - Failed operations

### Use Cases

- **Baseline performance** - Record expected throughput for your Redis setup
- **Regression testing** - Detect performance changes after updates
- **Capacity planning** - Determine if server can handle expected tag volume
