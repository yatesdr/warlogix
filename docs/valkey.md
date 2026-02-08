# Valkey/Redis Integration

WarLogix stores tag values in Valkey/Redis with optional Pub/Sub notifications and write-back queue support.


<img width="911" height="541" alt="image" src="https://github.com/user-attachments/assets/80851e84-b122-4718-b0a5-c3e858017371" />


## Configuration

```yaml
namespace: factory                  # Required: instance namespace

valkey:
  - name: LocalValkey
    enabled: true
    address: localhost:6379
    database: 0
    selector: line1                 # Optional: sub-namespace
    password: secret                # Optional
    use_tls: true                   # Optional
    key_ttl: 60s                    # Optional key expiration
    publish_changes: true           # Enable Pub/Sub
    enable_writeback: true          # Enable write-back queue
```

The `namespace` is a required top-level setting that identifies this WarLogix instance. The optional `selector` provides additional sub-organization within the namespace.

## Key Storage

### Tag Keys

Pattern: `{namespace}[:{selector}]:{plc}:tags:{tag}`

```json
{
  "factory": "factory:line1",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 42,
  "type": "DINT",
  "writable": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Health Keys

Pattern: `{namespace}[:{selector}]:{plc}:health`

Updated every 10 seconds:

```json
{
  "factory": "factory:line1",
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
| `{namespace}[:{selector}]:{plc}:changes` | Changes for specific PLC |
| `{namespace}[:{selector}]:_all:changes` | All changes across all PLCs |

**Subscribe example:**
```bash
redis-cli SUBSCRIBE factory:line1:MainPLC:changes factory:line1:_all:changes
```

## Write-Back Queue

When `enable_writeback: true`, write requests can be sent via a Redis LIST.

### Queue Key

`{namespace}[:{selector}]:writes`

### Send Write Request

```bash
redis-cli RPUSH factory:line1:writes '{"factory":"factory:line1","plc":"MainPLC","tag":"Counter","value":100}'
```

### Request Format

```json
{
  "factory": "factory:line1",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100
}
```

**Requirements:**
- The `factory` field must match the server's namespace (and selector if configured)
- Tag must be marked as `writable: true`

### Response Channel

Subscribe to: `{namespace}[:{selector}]:write:responses`

```bash
redis-cli SUBSCRIBE factory:line1:write:responses
```

**Success:**
```json
{
  "factory": "factory:line1",
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
  "factory": "factory:line1",
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
namespace: factory

valkey:
  - name: Primary
    enabled: true
    address: redis-primary:6379
    selector: primary

  - name: Replica
    enabled: true
    address: redis-replica:6379
    selector: replica
```

All enabled servers receive the same updates.

## CLI Examples

**Get tag value:**
```bash
redis-cli GET factory:line1:MainPLC:tags:Counter
```

**Get health:**
```bash
redis-cli GET factory:line1:MainPLC:health
```

**List all tags for a PLC:**
```bash
redis-cli KEYS "factory:line1:MainPLC:tags:*"
```

**Write a value:**
```bash
redis-cli RPUSH factory:line1:writes '{"factory":"factory:line1","plc":"MainPLC","tag":"Counter","value":100}'
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
