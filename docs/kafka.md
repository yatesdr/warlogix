# Kafka Integration

WarLogix publishes tag changes, health status, and event triggers to Apache Kafka. Optional writeback support allows external systems to write values to PLC tags via Kafka.

## Configuration

```yaml
namespace: factory                      # Required: instance namespace

kafka:
  - name: LocalKafka
    enabled: true
    brokers: [localhost:9092, localhost:9093]
    selector: line1                     # Optional: sub-namespace
    publish_changes: true
    use_tls: true                       # Optional
    tls_skip_verify: false              # Optional
    sasl_mechanism: SCRAM-SHA-256       # Optional: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
    username: user                      # Optional
    password: pass                      # Optional
    required_acks: -1                   # -1=all, 0=none, 1=leader
    max_retries: 3
    retry_backoff: 100ms

    # Writeback (optional)
    enable_writeback: true              # Enable consuming write requests
    consumer_group: warlogix-writers    # Default: warlogix-{name}-writers
    write_max_age: 2s                   # Ignore requests older than this
```

The `namespace` is a required top-level setting that identifies this WarLogix instance. The optional `selector` provides additional sub-organization within the namespace.

## Topics

WarLogix uses the following topics based on your configured `namespace` and optional `selector`:

| Topic | Direction | Content |
|-------|-----------|---------|
| `{namespace}[-{selector}]` | Publish | Tag value changes |
| `{namespace}[-{selector}].health` | Publish | PLC health status |
| `{namespace}[-{selector}]-writes` | Consume | Write requests (when `enable_writeback: true`) |
| `{namespace}[-{selector}]-write-responses` | Publish | Write responses (when `enable_writeback: true`) |

For example, if `namespace: factory` and `selector: line1`, messages are published to `factory-line1` and `factory-line1.health`. With writeback enabled, write requests are consumed from `factory-line1-writes` and responses published to `factory-line1-write-responses`.

### Tag Changes

When `publish_changes: true`, tag changes are published to `{namespace}[-{selector}]`:

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

Health messages are published to `{namespace}[-{selector}].health`:

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

## Writeback

When `enable_writeback: true`, WarLogix consumes write requests from Kafka and writes values to PLC tags. This enables bidirectional control via Kafka.

### Configuration

| Field | Default | Description |
|-------|---------|-------------|
| `enable_writeback` | `false` | Enable consuming write requests |
| `consumer_group` | `warlogix-{name}-writers` | Kafka consumer group ID |
| `write_max_age` | `2s` | Maximum age of requests to process |

Topics are derived from namespace:
- Write topic: `{namespace}[-{selector}]-writes`
- Response topic: `{namespace}[-{selector}]-write-responses`

### Write Request Format

Publish JSON messages to `{namespace}[-{selector}]-writes`:

```json
{
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100,
  "request_id": "optional-correlation-id"
}
```

| Field | Required | Description |
|-------|----------|-------------|
| `plc` | Yes | PLC name as configured in WarLogix |
| `tag` | Yes | Tag name (must be marked as `writable: true`) |
| `value` | Yes | Value to write (JSON type is auto-converted) |
| `request_id` | No | Correlation ID echoed in response |

**Message key**: Use `{plc}.{tag}` as the Kafka message key for proper deduplication.

### Write Response Format

Responses are published to `{namespace}[-{selector}]-write-responses` for **every request**:

```json
{
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100,
  "request_id": "optional-correlation-id",
  "success": true,
  "error": "",
  "skipped": false,
  "deduplicated": false,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

| Field | Description |
|-------|-------------|
| `success` | `true` if write completed successfully |
| `error` | Error message if `success` is `false` |
| `skipped` | `true` if request was expired (too old) |
| `deduplicated` | `true` if request was replaced by a newer write to same tag |
| `timestamp` | When the response was generated |

### Batch Processing and Deduplication

**Important:** Write requests are batched and deduplicated before execution. This means **not every write request will be executed** - only the most recent request per tag within each batch window.

#### How Batching Works

```
Timeline (250ms batch window):
─────────────────────────────────────────────────────────────────►
  0ms      50ms     100ms    150ms    200ms    250ms (batch fires)
   │        │         │        │        │         │
   ▼        ▼         ▼        ▼        ▼         ▼
Counter=5  Counter=10        Counter=15         Speed=100
   │        │                  │                  │
   └────────┴─── DISCARDED ────┘                  │
                               │                  │
                               ▼                  ▼
                        Counter=15 ◄─── EXECUTED ─► Speed=100
```

#### Processing Steps

1. **Batch collection (250ms)** - Requests are buffered as they arrive
2. **Deduplication** - For each `plc.tag`, only the **latest** request is kept; earlier requests are marked as deduplicated
3. **Age filtering** - Requests older than `write_max_age` are marked as expired
4. **Execution** - Remaining writes are executed sequentially
5. **Response** - **Every request receives a response** indicating its outcome

#### Deduplication Behavior

When multiple writes to the same tag arrive within a batch window, only the **latest** value is written to the PLC:

| Scenario | Outcome |
|----------|---------|
| Counter=5, then Counter=10 arrives | Counter=5 deduplicated (not written), Counter=10 executed |
| 10 rapid writes to same tag | First 9 deduplicated, only last one executed |
| Writes to different tags | All executed (no deduplication across tags) |

**Every request receives a response.** Deduplicated requests receive a response with `deduplicated: true` so your application knows the write was superseded.

#### Response Types

| Request Outcome | `success` | `skipped` | `deduplicated` | `error` |
|-----------------|-----------|-----------|----------------|---------|
| Executed successfully | `true` | `false` | `false` | (empty) |
| Execution failed | `false` | `false` | `false` | error message |
| Expired (too old) | `false` | `true` | `false` | "request expired..." |
| Deduplicated (replaced) | `false` | `false` | `true` | "request superseded..." |

**Deduplicated response example:**
```json
{
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 50,
  "success": false,
  "error": "request superseded by newer write to same tag",
  "deduplicated": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

#### Why This Design?

This behavior is intentional for PLC safety:

1. **Prevents PLC hammering** - Rapid writes to the same tag are coalesced
2. **Startup protection** - If WarLogix restarts with a backlog of queued writes, only the latest value per tag is written
3. **Network hiccups** - Burst of retried writes won't cause rapid PLC writes
4. **Final value wins** - The PLC ends up with the most recent intended value
5. **Full visibility** - Every request gets a response so you know exactly what happened

#### If You Need Every Write Executed

If your application requires every write to be executed (not just the latest):

1. **Check responses** - Monitor for `deduplicated: true` responses to detect when writes were coalesced
2. **Wait for response** - Send one write, wait for response, then send next
3. **Use unique tags** - Write to indexed tags (e.g., `Command[0]`, `Command[1]`)
4. **Use MQTT or REST** - These don't batch/deduplicate writes

### Stale Request Handling

Requests older than `write_max_age` (default 2 seconds) are marked as expired and not executed.

**Why requests expire:**
- WarLogix was stopped and restarted with old messages in Kafka
- Network delay caused messages to arrive late
- Kafka consumer fell behind due to processing delays

**Expired response example:**
```json
{
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100,
  "success": false,
  "error": "request expired (age: 5.2s, max: 2s)",
  "skipped": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

**Configuring max age:**
```yaml
kafka:
  - name: MainCluster
    write_max_age: 5s    # Increase if network latency is high
```

### Debug Logging

Enable debug logging to diagnose deduplication issues:

```bash
warlogix --log-debug=kafka
```

**Key log messages:**

```
# Request received
[Kafka] [Consumer] Received write request: partition=0 offset=42 key=MainPLC.Counter
[Kafka] [Consumer] Payload: {"plc":"MainPLC","tag":"Counter","value":50}

# Request deduplicated (replaced by newer)
[Kafka] [Consumer] DEDUP DISCARD: MainPLC/Counter value=50 (offset=42, age=50ms) replaced by value=100 (offset=45)

# Batch processing
[Kafka] [Consumer] Processing batch: 10 received, 7 deduplicated, 3 to execute
[Kafka] [Consumer] Sending deduplicated response for MainPLC/Counter value=50 (replaced by newer request)
[Kafka] [Consumer] Publishing response to factory-line1-write-responses: key=MainPLC.Counter success=false
[Kafka] [Consumer] Executing write: MainPLC/Counter = 100 (type: float64)
[Kafka] [Consumer] Write successful: MainPLC/Counter = 100
[Kafka] [Consumer] Batch complete: 2 succeeded, 0 failed, 1 expired, 7 deduplicated
```

### Consumer Group Behavior

WarLogix uses Kafka consumer groups for coordinated consumption:

- **Single consumer** - Only one WarLogix instance processes each write request
- **High availability** - Multiple instances share the consumer group for failover
- **Offset tracking** - Processed messages are committed to Kafka

If you run multiple WarLogix instances, they will share write processing (each request processed once). Use different consumer group names if you need separate instances.

### Producer Example

**Python:**
```python
from kafka import KafkaProducer
import json

producer = KafkaProducer(
    bootstrap_servers=['localhost:9092'],
    value_serializer=lambda v: json.dumps(v).encode('utf-8'),
    key_serializer=lambda k: k.encode('utf-8')
)

# Send write request
producer.send(
    'factory-line1-writes',
    key='MainPLC.Counter',
    value={
        'plc': 'MainPLC',
        'tag': 'Counter',
        'value': 100,
        'request_id': 'req-123'
    }
)
producer.flush()
```

**kafkacat:**
```bash
echo '{"plc":"MainPLC","tag":"Counter","value":100}' | \
  kafkacat -b localhost:9092 -t factory-line1-writes -P -k "MainPLC.Counter"
```

### Consumer Example (Responses)

**Python:**
```python
from kafka import KafkaConsumer
import json

consumer = KafkaConsumer(
    'factory-line1-write-responses',
    bootstrap_servers=['localhost:9092'],
    value_deserializer=lambda v: json.loads(v.decode('utf-8'))
)

for message in consumer:
    resp = message.value
    if resp['success']:
        print(f"Write succeeded: {resp['plc']}/{resp['tag']} = {resp['value']}")
    else:
        print(f"Write failed: {resp['error']}")
```

**kafkacat:**
```bash
kafkacat -b localhost:9092 -t factory-line1-write-responses -C
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

When `auto_create_topics` is enabled (default: true), topics are created automatically before the first publish. WarLogix explicitly creates topics via the Kafka Admin API rather than relying on broker-side `auto.create.topics.enable`, which may be disabled on production clusters.

Topics are created with:
- 1 partition (default)
- Replication factor of 1

For production deployments, pre-create topics with appropriate partition counts and replication factors.

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
namespace: factory

kafka:
  - name: Production
    enabled: true
    brokers: [kafka-prod:9092]
    selector: prod
    publish_changes: true

  - name: Analytics
    enabled: true
    brokers: [kafka-analytics:9092]
    selector: analytics
    publish_changes: true
```

## Consumer Examples

**kafkacat - Consume tag changes:**
```bash
kafkacat -b localhost:9092 -t factory-line1 -C
```

**kafkacat - Consume health:**
```bash
kafkacat -b localhost:9092 -t factory-line1.health -C
```

**kafka-console-consumer:**
```bash
kafka-console-consumer.sh --bootstrap-server localhost:9092 --topic factory-line1
```

## Stress Testing

Use the built-in stress test to benchmark your Kafka broker and detect publishing regressions:

```bash
warlogix --stress-test-republishing
```

This runs a 10-second stress test against all enabled Kafka clusters in your configuration, publishing simulated PLC tag data to a test topic (`warlogix-test-stress`).

### Options

| Flag | Default | Description |
|------|---------|-------------|
| `--test-duration` | 10s | Duration of each test |
| `--test-tags` | 100 | Number of simulated tags |
| `--test-plcs` | 50 | Number of simulated PLCs |
| `-y` | false | Skip confirmation prompt |

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
