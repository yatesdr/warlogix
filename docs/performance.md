# Performance Guide

This guide covers republishing performance, PLC read optimization, and tuning recommendations.

## Republishing Performance

WarLogix republishes PLC tag values to Kafka, MQTT, and Valkey/Redis. Use the built-in stress test to measure your broker throughput:

```bash
warlogix --stress-test-republishing
```

### Benchmark Results

Test conditions: 50 PLCs × 100 tags = 5,000 total tags, 10-second duration, localhost brokers. Kafka configured with 2 partitions.

| Broker | Throughput | Messages | Latency (avg) | Status |
|--------|-----------|----------|---------------|--------|
| Kafka | 290,805 msg/s | 2,969,132 | (batched) | PASS |
| MQTT | 32,521 msg/s | 325,210 | 30µs | PASS |
| Valkey | 44,912 msg/s | 449,127 | 22µs | PASS |

**Important:** These throughput differences reflect WarLogix's publishing implementation, not inherent broker capabilities:

| Broker | WarLogix Implementation | Why Different |
|--------|------------------------|---------------|
| **Kafka** | Batched async (100 msgs or 20ms) | Batching amortizes network overhead |
| **MQTT** | Synchronous QoS 1 per message | Waits for broker ACK each message |
| **Valkey** | Synchronous SET per message | Waits for Redis response each message |

All three technologies can handle much higher throughput with different client implementations. These numbers represent WarLogix's confirmed-delivery publishing rate for each broker type, ensuring no messages are lost.

**Detailed Results:**

```
Kafka/local:
  Address:    localhost:9092
  Duration:   10.21s
  Messages:   2,969,132 sent, 0 errors
  Throughput: 290,805 msg/s

MQTT/broker1:
  Address:    localhost:1883
  Duration:   10s
  Messages:   325,210 sent, 0 errors
  Throughput: 32,521 msg/s
  Latency:    avg: 30µs, p50: 29µs, p95: 41µs, p99: 57µs

Valkey/ValkeyServer1:
  Address:    127.0.0.1:6379
  Duration:   10s
  Messages:   449,127 sent, 0 errors
  Throughput: 44,912 msg/s
  Latency:    avg: 22µs, p50: 21µs, p95: 28µs, p99: 46µs
```

### Real-World Capacity

With change filtering (only publishing when values change), real-world message rates are typically much lower than stress test maximums.

**Note:** WarLogix enforces a minimum poll rate of 250ms (4Hz max) to protect PLCs. The default is 1000ms (1Hz), with 500ms (2Hz) being typical for active monitoring.

| Scenario | Tag Changes/sec | Kafka | MQTT | Valkey |
|----------|----------------|-------|------|--------|
| 10 PLCs @ 2Hz, 10% change rate | 200 | 1,454x headroom | 162x | 224x |
| 50 PLCs @ 2Hz, 10% change rate | 1,000 | 290x headroom | 32x | 45x |
| 100 PLCs @ 2Hz, 20% change rate | 4,000 | 72x headroom | 8x | 11x |

All three brokers provide substantial capacity for typical industrial deployments. Choose based on your infrastructure requirements, not raw throughput numbers.

## PLC Read Performance

PLC reads are typically the bottleneck, not republishing. Each PLC family has different performance characteristics.

### Allen-Bradley (Logix)

**Batching:** Yes - tags are batched into Multiple Service Packet requests.

| Mode | Batch Size | Typical Throughput |
|------|------------|-------------------|
| Connected (Forward Open) | Up to 50 tags | 500-2,000 tags/sec |
| Unconnected | Up to 5 tags | 50-200 tags/sec |

**How Batching Works:**

- **Scalar tags** (DINT, REAL, BOOL, etc.) are batched together efficiently
- **UDT/Structure tags** are read separately from scalars, but their members are batched (up to 50 members per request)
- **Arrays** require size-appropriate reads and may be fragmented for large arrays

**Optimization Tips:**

1. **Enable Connected Messaging** - WarLogix attempts Forward Open automatically. Connected mode is ~10x faster than unconnected.

2. **Prefer Scalars Over UDTs** - While UDT members are batched, reading individual scalar tags is slightly more efficient than UDT member expansion.

3. **Minimize Large Arrays** - Large arrays require fragmented reads. Consider reading only needed elements.

4. **Use Appropriate Poll Rates** - 250ms-1s is typical (250ms minimum). Faster polling increases PLC CPU load.

**Micro800 Note:** Micro800 series doesn't support Multiple Service Packet. All reads are individual, limiting throughput to ~50-100 tags/sec.

### Siemens S7

**Batching:** Yes - aggressive PDU-aware batching with up to 19 items per request.

| PLC Model | PDU Size | Typical Throughput |
|-----------|----------|-------------------|
| S7-1500 | 480 bytes | 500-1,500 tags/sec |
| S7-1200 | 240 bytes | 300-800 tags/sec |
| S7-300/400 | 240 bytes | 200-500 tags/sec |

**Optimization Tips:**

1. **Use Correct Rack/Slot** - S7-1200/1500 use slot 0; S7-300/400 typically use slot 2.

2. **Group by Data Block** - Tags in the same DB read more efficiently together.

3. **Use Aliases** - Configure meaningful aliases rather than raw addresses for maintainability.

4. **Minimize String Reads** - S7 STRING/WSTRING types are variable length and slower to parse.

5. **Large Arrays** - Arrays exceeding PDU size are automatically chunked, but this adds round-trips.

### Omron FINS (CS/CJ/CP Series)

**Batching:** Yes - contiguous address grouping and multi-memory area reads.

| Mode | Batch Size | Typical Throughput |
|------|------------|-------------------|
| Contiguous bulk read | Up to 998 words | 500-1,500 tags/sec |
| Multi-memory read (0x0104) | Up to 64 areas | 300-800 tags/sec |
| Individual reads (fallback) | 1 tag | 50-200 tags/sec |

**Supported PLC Series:** CS1, CJ1, CJ2, CP1, CV, and other FINS-compatible PLCs.

**How Batching Works:**

1. **Contiguous Address Grouping** - Sequential addresses in the same memory area (e.g., DM100, DM101, DM102) are combined into a single bulk read request. FINS supports up to 998 words per read.

2. **Multi-Memory Area Read** - Non-contiguous addresses are batched using FINS command 0x0104, which reads multiple areas in a single request (up to 64 areas per request, model-dependent).

3. **Automatic Optimization** - WarLogix automatically groups and batches addresses for optimal performance.

**Transport Selection:**

| Transport | Reliability | Speed | When Used |
|-----------|-------------|-------|-----------|
| FINS/TCP | High | Faster | Tried first (default) |
| FINS/UDP | Moderate | Moderate | Fallback if TCP fails |

**Optimization Tips:**

1. **Group Related Tags** - Tags in the same memory area with sequential addresses batch most efficiently.

2. **Use TCP Transport** - FINS/TCP is more reliable and supports persistent connections.

3. **Appropriate Poll Rates** - 250ms-1s is typical. With batching, higher tag counts are now practical.

4. **Configure Contiguous Blocks** - Reading DM100-DM199 (100 words) takes the same time as reading DM100 alone.

### Omron EIP/CIP (NJ/NX Series)

**Batching:** Yes - Multiple Service Packet (MSP) batching with optional connected messaging.

| Mode | Batch Size | Typical Throughput |
|------|------------|-------------------|
| Connected (Forward Open) | Up to 50 tags | 500-2,000 tags/sec |
| Unconnected | Up to 20 tags | 200-500 tags/sec |
| Individual reads (fallback) | 1 tag | 100-300 tags/sec |

**Supported PLC Series:** NJ series (NJ101, NJ301, NJ501), NX series (NX1, NX102, NX502, NX702), and other EtherNet/IP-compatible Omron PLCs.

**How Batching Works:**

1. **Multiple Service Packet (MSP)** - Multiple CIP Read Tag requests are combined into a single request using service 0x0A. Up to 200 services per packet (CIP limit), but WarLogix uses 50 for connected and 20 for unconnected to ensure reliability.

2. **Connected Messaging** - Forward Open establishes a persistent CIP connection with larger payload sizes (4002 bytes vs 504 bytes), enabling more tags per batch.

3. **Automatic Fallback** - If a batch fails, individual reads are used for that batch automatically.

**Tag Discovery:**

- Uses Get Instance Attribute List (service 0x55) for efficient paginated discovery
- Discovers hundreds of tags per request instead of instance-by-instance
- Automatically filters system/internal tags (prefixed with `_` or `$`)
- Falls back to legacy discovery for PLCs that don't support 0x55

**Protocol Notes:**

- Uses CIP (Common Industrial Protocol) over EtherNet/IP, same as Allen-Bradley Logix
- Symbolic tag addressing (tag names, not memory addresses)
- Supports tag discovery (automatic enumeration of available tags)
- Port 44818 (standard EtherNet/IP port)
- Little-endian byte order (same as Logix, unlike FINS which is big-endian)

**Optimization Tips:**

1. **Enable Connected Messaging** - Forward Open significantly improves throughput for large tag counts.

2. **Use EIP for NJ/NX** - EIP provides better performance, symbolic addressing, and tag discovery compared to FINS.

3. **Batch-Friendly Tags** - Scalar tags (DINT, REAL, BOOL) batch efficiently. Large arrays or structures may require individual reads.

4. **Appropriate Poll Rates** - With batching, 250-500ms poll rates are now practical for large tag counts.

### Beckhoff TwinCAT (ADS)

**Batching:** Yes - SumUp Read batches unlimited symbols in single request.

| Mode | Typical Throughput | Improvement |
|------|-------------------|-------------|
| SumUp Read (batched) | 1,000-5,000 tags/sec | Baseline |
| Individual reads | 30-100 tags/sec | 98% slower |

**Optimization Tips:**

1. **Let Batching Work** - WarLogix uses SumUp Read (IndexGroup 0xF080) automatically. No configuration needed.

2. **Symbol Discovery** - First connection discovers all symbols. Subsequent reads use cached handles.

3. **Minimize Symbol Count** - While batching is efficient, fewer symbols means faster discovery.

4. **Direct Addressing** - WarLogix uses direct addressing (0x4040) for maximum compatibility.

**Performance Note:** ADS has the largest gap between batched and individual reads. A 33-tag read improved from ~300ms to ~6ms with SumUp Read optimization.

## Comparative Summary

| PLC Family | Batching | Relative Speed | Best For |
|------------|----------|---------------|----------|
| **Allen-Bradley Logix** | MSP (50 tags/batch) | Fast | Mixed scalar/array workloads |
| **Siemens S7** | PDU-aware (19 items) | Fast | Homogeneous DB reads |
| **Omron FINS** (CS/CJ/CP) | Contiguous + Multi-read | Fast | Address-based bulk reads |
| **Omron EIP** (NJ/NX) | MSP (50 tags/batch) | Fast | Symbolic tag access, discovery |
| **Beckhoff ADS** | SumUp (unlimited) | Fastest | Large tag counts |

## System Optimization

### Poll Rate Selection

WarLogix enforces a minimum poll rate of 250ms to protect PLCs from excessive polling. The default is 1000ms (1 second).

| Use Case | Recommended Poll Rate |
|----------|----------------------|
| Fast-changing process values | 250-500ms |
| Standard monitoring | 500ms-1s |
| Slow-changing values | 1-5s |
| Status/diagnostic data | 5-10s |

Faster poll rates increase:
- PLC CPU utilization
- Network traffic
- Broker message volume

### Network Optimization

1. **Local Brokers** - Colocate brokers with WarLogix for lowest latency.

2. **Dedicated Network** - Separate PLC traffic from IT traffic where possible.

3. **Jumbo Frames** - Enable jumbo frames (9000 MTU) for Kafka if supported.

4. **TCP Tuning** - Increase TCP buffer sizes for high-throughput scenarios.

### Broker Selection

Choose based on your use case, not throughput numbers (all have sufficient capacity for typical deployments):

| Broker | Best For | Key Features |
|--------|----------|--------------|
| **Kafka** | Event streaming, audit trails, multi-consumer | Durable, replayable, horizontally scalable |
| **MQTT** | IoT integration, simple pub/sub, bidirectional | Lightweight, write-back support, widely supported |
| **Valkey/Redis** | Real-time dashboards, caching, key-value lookup | Instant access by tag, Pub/Sub, write-back queue |

You can enable multiple brokers simultaneously - WarLogix publishes to all configured brokers in parallel.

### Change Filtering

WarLogix only publishes when values change. To reduce message volume:

1. **Reduce Precision** - Round floating-point values to reduce noise-driven changes.

2. **Deadband** - Configure deadband at the PLC level for analog values.

3. **Consolidate Tags** - Group related values into structures read atomically.

### Memory and CPU

- **Memory** - ~1KB per monitored tag for caching and state.
- **CPU** - Dominated by JSON serialization; ~1-2% per 1,000 tags at 2Hz.

For very large deployments (>10,000 tags), consider:
- Multiple WarLogix instances with tag partitioning
- Longer poll intervals for less-critical data
- Tiered polling (fast/slow groups)

## Stress Testing

Run the built-in stress test to establish your baseline:

```bash
# Default: 50 PLCs, 100 tags each, 10 seconds
warlogix --stress-test-republishing

# Custom parameters
warlogix --stress-test-republishing --test-duration 30s --test-plcs 100 --test-tags 200

# Skip confirmation prompt (for CI/scripts)
warlogix --stress-test-republishing -y
```

Save the baseline throughput values and re-run after configuration changes to detect regressions.

## Troubleshooting Performance

### Slow PLC Reads

1. Check PLC CPU load - high scan times indicate overload
2. Verify network latency with ping
3. Reduce tag count or poll rate
4. Check for connection mode (connected vs unconnected for Logix)

### Broker Backlog

1. Run stress test to verify broker capacity
2. Check broker disk I/O (Kafka) or memory (Valkey)
3. Increase batch sizes or flush intervals
4. Scale brokers horizontally if needed

### High Latency

1. Check network path between WarLogix and brokers
2. Verify TLS overhead if encryption enabled
3. Monitor broker consumer lag (Kafka)
4. Check for network congestion or packet loss
