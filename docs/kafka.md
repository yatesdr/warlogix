# Kafka Integration

WarLogix publishes tag changes, health status, and event triggers to Apache Kafka. Kafka is publish-only (no write-back support).

## Configuration

```yaml
kafka:
  - name: LocalKafka
    enabled: true
    brokers: [localhost:9092, localhost:9093]
    topic: plc-tags
    publish_changes: true
    use_tls: true                       # Optional
    tls_skip_verify: false              # Optional
    sasl_mechanism: SCRAM-SHA-256       # Optional: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
    username: user                      # Optional
    password: pass                      # Optional
    required_acks: -1                   # -1=all, 0=none, 1=leader
    max_retries: 3
    retry_backoff: 100ms
```

## Topics

WarLogix publishes to two topics based on your configured `topic` setting:

| Topic | Content |
|-------|---------|
| `{topic}` | Tag value changes |
| `{topic}.health` | PLC health status |

For example, if `topic: plc-tags`, messages are published to `plc-tags` and `plc-tags.health`.

### Tag Changes

When `publish_changes: true`, tag changes are published to `{topic}`:

```json
{
  "plc": "MainPLC",
  "tag": "Counter",
  "address": "DB1.DBD100",
  "value": 42,
  "type": "DINT",
  "writable": false,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

**Tag naming by PLC type:**

| PLC Family | `tag` field | `address` field | Message key |
|------------|-------------|-----------------|-------------|
| **Tag-based** (Logix, Micro800, Omron, Beckhoff) | Tag name | (empty) | `{plc}.{tag}` |
| **Memory-based** (S7) | Alias (if configured) or address | Memory address (uppercase) | `{plc}.{alias}` or `{plc}.{address}` |

For S7 PLCs, if you configure an alias for a tag, both the `tag` field and the message key will use the alias. This makes it easier to work with meaningful names rather than raw memory addresses.

**Example S7 with alias:**
```json
{
  "plc": "S7-1500",
  "tag": "ProductCount",
  "address": "DB1.DBD100",
  "value": 42,
  "type": "DINT",
  "writable": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```
Message key: `S7-1500.ProductCount`

**Example S7 without alias:**
```json
{
  "plc": "S7-1500",
  "tag": "DB1.DBD100",
  "address": "DB1.DBD100",
  "value": 42,
  "type": "DINT",
  "writable": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```
Message key: `S7-1500.DB1.DBD100`

### Health Status

Health messages are published to `{topic}.health`:

```json
{
  "plc": "MainPLC",
  "driver": "logix",
  "online": true,
  "status": "connected",
  "error": "",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

Message key: `{plc}`

Health publishing can be disabled per-PLC with `health_check_enabled: false`.

### Trigger Events

Event triggers publish to their configured topic (see [Triggers](triggers.md)):

```json
{
  "trigger": "ProductComplete",
  "timestamp": "2024-01-15T10:30:00.123456789Z",
  "sequence": 42,
  "plc": "MainPLC",
  "metadata": {
    "line": "Line1"
  },
  "data": {
    "ProductID": 12345,
    "Quantity": 100
  }
}
```

## Authentication

### No Authentication

```yaml
kafka:
  - name: Local
    brokers: [localhost:9092]
```

### SASL/PLAIN

```yaml
kafka:
  - name: SASL
    brokers: [kafka:9092]
    sasl_mechanism: PLAIN
    username: user
    password: pass
```

### SASL/SCRAM

```yaml
kafka:
  - name: SCRAM
    brokers: [kafka:9092]
    sasl_mechanism: SCRAM-SHA-256    # or SCRAM-SHA-512
    username: user
    password: pass
```

### TLS

```yaml
kafka:
  - name: TLS
    brokers: [kafka:9093]
    use_tls: true
    tls_skip_verify: false    # Set true for self-signed certs
```

### TLS + SASL

```yaml
kafka:
  - name: Secure
    brokers: [kafka:9093]
    use_tls: true
    sasl_mechanism: SCRAM-SHA-512
    username: user
    password: pass
```

## Acknowledgment Settings

| Value | Meaning |
|-------|---------|
| `-1` | All replicas must acknowledge (strongest durability) |
| `0` | No acknowledgment (fire and forget) |
| `1` | Leader must acknowledge |

## Performance

WarLogix uses batched publishing for high-throughput Kafka delivery. Messages are collected per topic and flushed together, reducing round-trips and improving throughput.

### Batching Behavior

| Setting | Value | Description |
|---------|-------|-------------|
| Batch size | 100 messages | Maximum messages per batch |
| Batch timeout | 20ms | Flush interval for partial batches |
| Writer batch size | 100 messages | kafka-go writer internal batching |
| Writer batch timeout | 10ms | Writer flush interval |

Messages are batched at two levels:
1. **Manager level** - Collects messages per topic, flushes every 20ms or at 100 messages
2. **Writer level** - kafka-go's internal batching provides additional buffering

### Topic Auto-Creation

When `auto_create_topics` is enabled (default), topics are created automatically on first publish using the Kafka broker's auto-create feature. This avoids the overhead of explicit topic creation calls.

### Throughput

Typical throughput depends on broker latency and message size:

| Scenario | Expected Throughput |
|----------|---------------------|
| Local broker | 1,000 - 5,000 msg/s |
| Network broker | 500 - 2,000 msg/s |
| Many PLCs, many tags | Scales with batching |

For maximum throughput:
- Use `required_acks: 1` instead of `-1`
- Ensure broker is on a low-latency network
- Monitor with debug logging: `-debug -debug-filter kafka`

## Multiple Clusters

Configure multiple Kafka clusters:

```yaml
kafka:
  - name: Production
    enabled: true
    brokers: [kafka-prod:9092]
    topic: factory-tags
    publish_changes: true

  - name: Analytics
    enabled: true
    brokers: [kafka-analytics:9092]
    topic: plc-metrics
    publish_changes: true
```

## Consumer Examples

**kafkacat - Consume tag changes:**
```bash
kafkacat -b localhost:9092 -t plc-tags -C
```

**kafkacat - Consume health:**
```bash
kafkacat -b localhost:9092 -t plc-tags.health -C
```

**kafka-console-consumer:**
```bash
kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic plc-tags
```

## Stress Testing

Use the built-in stress test to benchmark your Kafka broker and detect publishing regressions:

```bash
warlogix --test-brokers
```

This runs a 10-second stress test against all enabled Kafka clusters in your configuration, publishing simulated PLC tag data to a test topic (`warlogix-test-stress`).

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `--test-duration` | 10s | Duration of each test |
| `--test-tags` | 100 | Number of simulated tags |
| `--test-plcs` | 5 | Number of simulated PLCs |

### Example Output

```
╔══════════════════════════════════════════════════════════════════╗
║                         TEST RESULTS                             ║
╚══════════════════════════════════════════════════════════════════╝

  ┌─────────┬────────────────┬────────────────┬──────────────┬────────┐
  │ Type    │ Name           │ Throughput     │ Messages     │ Status │
  ├─────────┼────────────────┼────────────────┼──────────────┼────────┤
  │ Kafka   │ local          │       2341 msg/s │        23415 │ ✓ PASS │
  └─────────┴────────────────┴────────────────┴──────────────┴────────┘

  Kafka/local:
    Address:    localhost:9092
    Duration:   10.001s
    Messages:   23415 sent, 0 errors
    Throughput: 2341.3 msg/s
    Latency:
      avg: 1.2ms, p50: 980µs, p95: 2.1ms, p99: 5.3ms, max: 12ms
```

### Use Cases

- **Baseline performance** - Record expected throughput for your broker setup
- **Regression testing** - Detect publishing performance changes after updates
- **Capacity planning** - Determine if broker can handle expected tag volume
