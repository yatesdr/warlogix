# Performance Guide

This guide covers republishing performance, PLC read optimization, and tuning recommendations.

## Republishing Performance

WarLogix republishes PLC tag values to Kafka, MQTT, and Valkey/Redis. Use the built-in stress test to measure your broker throughput:

```bash
warlogix --stress-test-republishing
```

### Benchmark Results

Test conditions: 50 PLCs × 100 tags = 5,000 total tags, 10-second duration, localhost brokers.

| Broker | Throughput | Messages | Latency (avg) | Status |
|--------|-----------|----------|---------------|--------|
| Kafka | 290,805 msg/s | 2,969,132 | (batched) | PASS |
| MQTT | 32,521 msg/s | 325,210 | 30µs | PASS |
| Valkey | 44,912 msg/s | 449,127 | 22µs | PASS |

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

With change filtering (only publishing when values change), real-world message rates are typically much lower:

| Scenario | Tag Changes/sec | Kafka | MQTT | Valkey |
|----------|----------------|-------|------|--------|
| 10 PLCs @ 10Hz, 10% change rate | 1,000 | 290x headroom | 32x | 45x |
| 50 PLCs @ 10Hz, 10% change rate | 5,000 | 58x headroom | 6x | 9x |
| 100 PLCs @ 10Hz, 20% change rate | 20,000 | 14x headroom | 1.6x | 2.2x |

For high-volume deployments, Kafka provides the most headroom due to its batched publishing model.

## PLC Read Performance

PLC reads are typically the bottleneck, not republishing. Each PLC family has different performance characteristics.

### Allen-Bradley (Logix)

**Batching:** Yes - scalar tags are batched into Multiple Service Packet requests.

| Mode | Batch Size | Typical Throughput |
|------|------------|-------------------|
| Connected (Forward Open) | Up to 50 tags | 500-2,000 tags/sec |
| Unconnected | Up to 5 tags | 50-200 tags/sec |

**Optimization Tips:**

1. **Enable Connected Messaging** - WarLogix attempts Forward Open automatically. Connected mode is ~10x faster than unconnected.

2. **Group Scalar Tags** - Scalars (DINT, REAL, BOOL) batch efficiently. Arrays and structures read individually.

3. **Minimize Large Arrays** - Large arrays require fragmented reads. Consider reading only needed elements.

4. **Use Appropriate Poll Rates** - 100-500ms is typical. Faster polling increases PLC CPU load.

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

### Omron FINS

**Batching:** No - each tag is read individually.

| Transport | Typical Throughput |
|-----------|-------------------|
| FINS/TCP | 50-200 tags/sec |
| FINS/UDP | 30-150 tags/sec |
| EIP (NJ/NX) | 100-300 tags/sec |

**Optimization Tips:**

1. **Use TCP over UDP** - FINS/TCP is more reliable and often faster due to better error handling.

2. **Minimize Tag Count** - Without batching, read time scales linearly with tag count.

3. **Use EIP for NJ/NX** - NJ/NX series support EtherNet/IP which may perform better than FINS.

4. **Group Memory Areas** - Reads within the same area (DM, CIO, WR) are slightly more efficient.

5. **Consider Larger Poll Intervals** - 500ms-1s may be more appropriate given individual read limitations.

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
| **Allen-Bradley Logix** | Scalars only | Fast | Mixed scalar/array workloads |
| **Siemens S7** | Aggressive | Fast | Homogeneous DB reads |
| **Omron FINS** | None | Slow | Small tag counts (<50) |
| **Beckhoff ADS** | Full (SumUp) | Fastest | Large tag counts |

## System Optimization

### Poll Rate Selection

| Use Case | Recommended Poll Rate |
|----------|----------------------|
| Fast-changing process values | 50-100ms |
| Standard monitoring | 100-500ms |
| Slow-changing values | 1-5s |
| Status/diagnostic data | 5-30s |

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

| Broker | Strengths | Weaknesses |
|--------|-----------|------------|
| **Kafka** | Highest throughput, durable, scalable | More complex setup |
| **MQTT** | Simple, lightweight, bidirectional | Lower throughput |
| **Valkey/Redis** | Key-value access, Pub/Sub, write-back | Memory-bound |

For maximum throughput, use Kafka. For simplicity, use MQTT. For real-time key-value access with write-back, use Valkey.

### Change Filtering

WarLogix only publishes when values change. To reduce message volume:

1. **Reduce Precision** - Round floating-point values to reduce noise-driven changes.

2. **Deadband** - Configure deadband at the PLC level for analog values.

3. **Consolidate Tags** - Group related values into structures read atomically.

### Memory and CPU

- **Memory** - ~1KB per monitored tag for caching and state.
- **CPU** - Dominated by JSON serialization; ~1-5% per 1,000 tags at 10Hz.

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
