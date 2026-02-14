# Best Practices

This guide covers recommended practices for deploying and operating WarLink in production environments.

## Namespace Design

The namespace is the foundation of your data organization. Choose wisely—changing it later requires updating all consumers.

### Naming Conventions

| Pattern | Example | Use Case |
|---------|---------|----------|
| Location-based | `plant-chicago`, `factory-east` | Multi-site deployments |
| Function-based | `packaging`, `assembly`, `quality` | Single site, multiple lines |
| Hierarchical | `chicago-packaging-line1` | Large deployments needing granularity |

**Recommendations:**

- Use lowercase with hyphens (URL-safe, works across all services)
- Keep it short but meaningful (appears in every topic/key)
- Plan for growth—don't paint yourself into a corner
- Document your naming convention

### Selector Strategy

Selectors provide sub-namespacing within a single WarLink instance:

```yaml
namespace: factory-east

mqtt:
  - name: production
    selector: prod          # Topics: factory-east/prod/...
  - name: quality
    selector: quality       # Topics: factory-east/quality/...
```

**When to use selectors:**
- Route different data types to different topics
- Separate production data from quality data
- Enable per-broker topic customization

**When NOT to use selectors:**
- If you only have one broker per service type
- If all data should go to the same topic

---

## Tag Selection Strategy

### What to Publish

**DO publish:**
- Process values needed for dashboards/reporting
- Alarm states and fault codes
- Production counters and KPIs
- Setpoints (for monitoring, not control)
- Quality measurements

**DON'T publish:**
- High-frequency internal data (loop variables, timers)
- Security-sensitive data (passwords, keys)
- Data that changes faster than you can consume it
- Redundant data (same value from multiple sources)

### Change Detection Optimization

Use `ignore_changes` to reduce noise from volatile UDT members:

```yaml
tags:
  - name: MachineStatus
    enabled: true
    ignore_changes:
      - Heartbeat        # Changes every scan
      - CycleTimer       # Changes continuously
      - Timestamp        # Changes every scan
      - SequenceNumber   # Changes frequently
```

The ignored members are still included in published data—they just don't trigger republishing.

### Per-Service Filtering

Not every tag needs to go everywhere:

```yaml
tags:
  - name: HighSpeedCounter
    enabled: true
    no_mqtt: true       # Too frequent for MQTT
    no_valkey: true     # Don't need in Redis
    # Still goes to Kafka (handles high throughput)

  - name: AlarmStatus
    enabled: true
    no_kafka: true      # Alarms go to MQTT/Valkey only
```

---

## Poll Rate Tuning

### Guidelines

| Data Type | Recommended Poll Rate | Rationale |
|-----------|----------------------|-----------|
| Alarms, states | 250-500ms | Fast enough for alerting |
| Process values | 500ms-1s | Balance of responsiveness and load |
| Energy/utility | 1-5s | Slower-changing data |
| Diagnostic data | 5-10s | Background monitoring |

### Per-PLC Configuration

Different PLCs can have different poll rates:

```yaml
poll_rate: 1s  # Global default

plcs:
  - name: CriticalPLC
    poll_rate: 250ms    # Fast polling for this PLC

  - name: UtilityPLC
    poll_rate: 5s       # Slower for non-critical data
```

### Impact Considerations

Faster polling:
- Increases network traffic
- Increases PLC CPU load
- May impact PLC scan time
- Provides more responsive data

Slower polling:
- Reduces resource usage
- May miss fast transients
- Better for stable process values

---

## TagPack Design

### Grouping Strategies

**By Function:**
```yaml
tag_packs:
  - name: motor_status
    members:
      - plc: Line1, tag: Motor1_Running
      - plc: Line1, tag: Motor1_Speed
      - plc: Line1, tag: Motor1_Current
      - plc: Line1, tag: Motor1_Temp
```

**By Consumer:**
```yaml
tag_packs:
  - name: mes_data
    members:
      - plc: Line1, tag: PartCount
      - plc: Line1, tag: CycleTime
      - plc: Line2, tag: PartCount
      - plc: Line2, tag: CycleTime
```

**By Timing Requirement:**
```yaml
tag_packs:
  - name: synchronized_snapshot
    members:
      - plc: PLC1, tag: Measurement1
      - plc: PLC1, tag: Measurement2
      - plc: PLC2, tag: Measurement3  # Cross-PLC
```

### Ignore Strategy

```yaml
tag_packs:
  - name: production_status
    members:
      - plc: Line1, tag: RunningState     # Triggers publish
      - plc: Line1, tag: PartCount        # Triggers publish
      - plc: Line1, tag: Timestamp
        ignore_changes: true              # Included but doesn't trigger
      - plc: Line1, tag: Heartbeat
        ignore_changes: true              # Included but doesn't trigger
```

---

## Trigger Design

### Condition Selection

Choose trigger conditions carefully:

**Good trigger conditions:**
- Boolean flags set by PLC logic (`PartComplete`, `BatchDone`)
- Counter increments (`PartCount > LastPartCount`)
- State transitions (`State == 'Complete'`)

**Avoid:**
- Rapidly changing values (will fire too often)
- Values that oscillate (will fire repeatedly)
- Values dependent on timing (race conditions)

### Debouncing

Set appropriate debounce times:

```yaml
triggers:
  - name: part_complete
    debounce_ms: 100    # Fast response, minimal debounce

  - name: batch_complete
    debounce_ms: 500    # Longer debounce for batch operations

  - name: alarm_capture
    debounce_ms: 1000   # Prevent spam on chattering alarms
```

### Acknowledgment Pattern

For reliable handshaking with PLCs:

```yaml
triggers:
  - name: production_event
    trigger_tag: DataReady
    ack_tag: DataAck        # WarLink writes 1 (success) or -1 (error)
```

PLC logic should:
1. Set trigger tag when data is ready
2. Wait for ack tag to become non-zero
3. Handle success (1) or error (-1)
4. Reset both tags for next cycle

---

## High Availability

### Broker Redundancy

Configure multiple brokers for resilience:

```yaml
mqtt:
  - name: primary
    broker: mqtt1.example.com
    enabled: true
  - name: secondary
    broker: mqtt2.example.com
    enabled: true

kafka:
  - name: cluster1
    brokers:
      - kafka1.example.com:9092
      - kafka2.example.com:9092
      - kafka3.example.com:9092
    enabled: true
```

### Instance Redundancy

For critical applications, run redundant WarLink instances:

**Option 1: Active/Passive**
- Primary instance publishes
- Secondary instance on standby
- Manual or automated failover

**Option 2: Active/Active with Namespaces**
- Different namespaces for different PLCs
- Load distributed across instances
- No single point of failure

**Note:** Two WarLink instances cannot connect to the same PLC simultaneously for most PLC types (protocol limitation).

---

## Security

### Network Segmentation

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│   PLC Network   │────▶│    WarLink     │────▶│   IT Network    │
│  (Isolated OT)  │     │  (DMZ/Bridge)   │     │ (MQTT/Kafka/etc)│
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

- WarLink acts as the bridge between OT and IT
- Only WarLink needs access to both networks
- Reduces attack surface on PLC network

### Authentication

Always enable authentication for services:

```yaml
mqtt:
  - name: production
    username: warlink
    password: ${MQTT_PASSWORD}  # Use environment variable
    use_tls: true

kafka:
  - name: production
    sasl_mechanism: SCRAM-SHA-512
    username: warlink
    password: ${KAFKA_PASSWORD}
    use_tls: true

valkey:
  - name: production
    password: ${REDIS_PASSWORD}
    use_tls: true
```

### Daemon Mode Security

For SSH access:

```bash
# Use key-based authentication (recommended)
./warlink -d --ssh-keys /etc/warlink/authorized_keys

# If using passwords, use strong passwords
./warlink -d --ssh-pass "$(openssl rand -base64 32)"
```

### Write-Back Security

Limit write-back to specific tags:

```yaml
tags:
  - name: Setpoint
    writable: false     # Read-only

  - name: AckFlag
    writable: true      # Only this tag is writable
```

---

## Monitoring

### Health Checks

WarLink publishes health status for monitoring:

**REST:**
```bash
curl http://localhost:8080/MainPLC/health
```

**MQTT:**
```bash
mosquitto_sub -t "factory/+/health"
```

**Valkey:**
```bash
redis-cli GET factory:MainPLC:health
```

### Metrics to Monitor

| Metric | Source | Alert Threshold |
|--------|--------|-----------------|
| PLC connection status | Health endpoint | Any disconnect |
| Message publish rate | Broker metrics | Drop > 50% |
| Publish errors | Debug logs | Any errors |
| Memory usage | System metrics | > 80% |
| Trigger fire count | REST API / logs | Unexpected changes |

### Log Aggregation

Send logs to a central system:

```bash
# Systemd journal (captured automatically)
journalctl -u warlink -f

# File logging
./warlink --log /var/log/warlink/warlink.log
```

---

## Deployment Checklist

### Pre-Deployment

- [ ] Namespace naming convention documented
- [ ] All PLCs tested individually
- [ ] Tags selected and verified
- [ ] Brokers configured and tested
- [ ] Authentication configured
- [ ] Network connectivity verified
- [ ] Firewall rules in place

### Deployment

- [ ] Configuration file deployed
- [ ] Service installed (systemd/OpenRC)
- [ ] Auto-start enabled
- [ ] SSH access configured (if daemon mode)
- [ ] Monitoring configured
- [ ] Alerts configured

### Post-Deployment

- [ ] All PLCs connecting successfully
- [ ] Messages appearing in brokers
- [ ] Health checks passing
- [ ] Consumers receiving data
- [ ] Documentation updated
- [ ] Runbook created for operations team
