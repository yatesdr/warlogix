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

### Tag Changes

When `publish_changes: true`, tag changes are published to `{topic}`:

```json
{
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 42,
  "type": "DINT",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

Message key: `{plc}/{tag}` (enables partitioning by tag)

### Health Status

Health messages are published every 10 seconds to `{topic}.health`:

```json
{
  "plc": "MainPLC",
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
