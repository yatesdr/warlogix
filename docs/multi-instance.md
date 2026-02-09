# Multi-Instance Deployment

This guide explains how to safely run multiple WarLink instances that connect to shared message brokers (MQTT, Valkey/Redis, Kafka). Proper configuration prevents write requests from being processed by the wrong instance.

## The Problem

Consider two factories, each with a WarLink instance:

```
┌─────────────────────┐         ┌─────────────────────┐
│     Factory A       │         │     Factory B       │
│  ┌───────────────┐  │         │  ┌───────────────┐  │
│  │  WarLink #1  │  │         │  │  WarLink #2  │  │
│  └───────┬───────┘  │         │  └───────┬───────┘  │
│          │          │         │          │          │
│  ┌───────▼───────┐  │         │  ┌───────▼───────┐  │
│  │   Main PLC    │  │         │  │   Main PLC    │  │
│  │ 192.168.1.100 │  │         │  │ 192.168.1.100 │  │
│  └───────────────┘  │         │  └───────────────┘  │
│                     │         │                     │
│    NAT Gateway      │         │    NAT Gateway      │
└─────────┬───────────┘         └─────────┬───────────┘
          │                               │
          └───────────┬───────────────────┘
                      │
              ┌───────▼───────┐
              │  Colo Server  │
              │  ─────────────│
              │  MQTT Broker  │
              │  Redis/Valkey │
              │  Kafka Cluster│
              └───────────────┘
```

Both PLCs are named "Main PLC" and have IP 192.168.1.100 (common behind NAT). Both have a tag `Machine_Status.Clear_Fault` that's writable.

**Without proper configuration:**
- A write request to clear Factory A's fault might be processed by Factory B
- Tag publications from both factories would mix together
- Health status would overwrite each other

## The Solution: Unified Namespace

WarLink uses a single `namespace` field at the top level of the configuration to identify each instance. This namespace is used consistently across all services:

```yaml
namespace: factory-a    # Unique per instance
```

The namespace is automatically applied to:
- MQTT topics
- Valkey/Redis keys and channels
- Kafka topics

Optional `selector` fields on individual service configurations provide additional sub-organization within the namespace.

## Complete Configuration Example

### Factory A (Instance 1)

```yaml
# /etc/warlink/config.yaml on Factory A

namespace: factory-a                    # <-- UNIQUE per instance

plcs:
  - name: Main PLC
    driver: logix
    address: 192.168.1.100
    tags:
      - name: Machine_Status.Clear_Fault
        writable: true

mqtt:
  - name: Central
    enabled: true
    broker: mqtt.colo.example.com

valkey:
  - name: Central
    enabled: true
    address: redis.colo.example.com:6379
    enable_writeback: true

kafka:
  - name: Central
    enabled: true
    brokers: [kafka.colo.example.com:9092]
    enable_writeback: true
```

### Factory B (Instance 2)

```yaml
# /etc/warlink/config.yaml on Factory B

namespace: factory-b                    # <-- UNIQUE per instance

plcs:
  - name: Main PLC
    driver: logix
    address: 192.168.1.100
    tags:
      - name: Machine_Status.Clear_Fault
        writable: true

mqtt:
  - name: Central
    enabled: true
    broker: mqtt.colo.example.com

valkey:
  - name: Central
    enabled: true
    address: redis.colo.example.com:6379
    enable_writeback: true

kafka:
  - name: Central
    enabled: true
    brokers: [kafka.colo.example.com:9092]
    enable_writeback: true
```

## How Isolation Works

### MQTT

Each instance subscribes to its own write topic based on namespace:

```
Instance 1 subscribes to: factory-a/+/write
Instance 2 subscribes to: factory-b/+/write
```

To clear Factory A's fault:

```bash
mosquitto_pub -h mqtt.colo.example.com \
  -t "factory-a/Main PLC/write" \
  -m '{"topic":"factory-a","plc":"Main PLC","tag":"Machine_Status.Clear_Fault","value":true}'
```

- Instance 1 receives this (subscribed to `factory-a/+/write`)
- Instance 2 never sees it (subscribed to `factory-b/+/write`)

**Double protection:** The `topic` field in the JSON payload must match the instance's namespace. Even if a message somehow reached the wrong instance, it would be rejected.

### Valkey/Redis

Each instance uses its own write queue based on namespace:

```
Instance 1 pops from: factory-a:writes
Instance 2 pops from: factory-b:writes
```

To clear Factory A's fault:

```bash
redis-cli -h redis.colo.example.com RPUSH factory-a:writes \
  '{"factory":"factory-a","plc":"Main PLC","tag":"Machine_Status.Clear_Fault","value":true}'
```

- Instance 1 processes this (polling `factory-a:writes`)
- Instance 2 never sees it (polling `factory-b:writes`)

**Double protection:** The `factory` field in the JSON payload must match the instance's namespace.

### Kafka

Each instance consumes from its own write topic based on namespace:

```
Instance 1 consumes from: factory-a-writes
Instance 2 consumes from: factory-b-writes
```

To clear Factory A's fault:

```bash
echo '{"plc":"Main PLC","tag":"Machine_Status.Clear_Fault","value":true}' | \
  kafkacat -b kafka.colo.example.com:9092 \
  -t factory-a-writes \
  -P -k "Main PLC.Machine_Status.Clear_Fault"
```

- Instance 1 processes this (consuming `factory-a-writes`)
- Instance 2 never sees it (consuming `factory-b-writes`)

## Topic/Key Structure Summary

With proper namespacing, here's where data flows:

### Factory A

| Purpose | MQTT Topic | Valkey Key | Kafka Topic |
|---------|------------|------------|-------------|
| Tag values | `factory-a/Main PLC/tags/Machine_Status.Clear_Fault` | `factory-a:Main PLC:tags:Machine_Status.Clear_Fault` | `factory-a` |
| Health | `factory-a/Main PLC/health` | `factory-a:Main PLC:health` | `factory-a.health` |
| Write requests | `factory-a/Main PLC/write` | `factory-a:writes` | `factory-a-writes` |
| Write responses | `factory-a/Main PLC/write/response` | `factory-a:write:responses` | `factory-a-write-responses` |

### Factory B

| Purpose | MQTT Topic | Valkey Key | Kafka Topic |
|---------|------------|------------|-------------|
| Tag values | `factory-b/Main PLC/tags/Machine_Status.Clear_Fault` | `factory-b:Main PLC:tags:Machine_Status.Clear_Fault` | `factory-b` |
| Health | `factory-b/Main PLC/health` | `factory-b:Main PLC:health` | `factory-b.health` |
| Write requests | `factory-b/Main PLC/write` | `factory-b:writes` | `factory-b-writes` |
| Write responses | `factory-b/Main PLC/write/response` | `factory-b:write:responses` | `factory-b-write-responses` |

## Using Selectors

If you need additional sub-organization within a namespace (e.g., different production lines), use the `selector` field on individual services:

```yaml
namespace: factory-a

mqtt:
  - name: Line1
    broker: mqtt.colo.example.com
    selector: line1           # Topics: factory-a/line1/{plc}/...

  - name: Line2
    broker: mqtt.colo.example.com
    selector: line2           # Topics: factory-a/line2/{plc}/...
```

## What Happens Without Namespacing?

If both instances use the same namespace (e.g., both use `namespace: factory`):

### MQTT

Both instances subscribe to `factory/+/write`. When a write arrives:

1. **Both receive the message** (MQTT broadcasts to all subscribers)
2. Both check if they have a PLC named "Main PLC" - they do
3. Both attempt to execute the write
4. **Result:** Both PLCs get the write command (wrong!)

### Valkey

Both instances pop from `factory:writes`. When a write arrives:

1. **One instance wins** the `BLPOP` race (Redis is atomic)
2. The other instance never sees the message
3. **Result:** Unpredictable which factory processes the write (wrong!)

### Kafka

Both instances join the same consumer group. When a write arrives:

1. Kafka assigns partitions to consumers
2. **One instance receives the message** (whichever has that partition)
3. **Result:** Unpredictable which factory processes the write (wrong!)

## Naming Conventions

Choose a convention and stick to it across all factories:

| Convention | Namespace Example |
|------------|-------------------|
| Simple site ID | `site-001`, `site-002` |
| Geographic | `us-east-plant-1`, `eu-west-plant-2` |
| Customer/site | `acme-chicago`, `acme-detroit` |
| Functional | `assembly-line-a`, `packaging-line-b` |

## TUI Namespace Configuration

You can configure the namespace from the TUI by pressing `N` (Shift+N) to open the namespace modal. The modal provides:
- Current namespace display
- Input field for new namespace
- Validation feedback
- Save button (requires restart for changes to take effect)

The current namespace is always visible in the status bar at the bottom of the screen.

## Verification Checklist

Before deploying multiple instances:

- [ ] Each instance has a unique `namespace`
- [ ] Documentation records which namespace belongs to which site
- [ ] Monitoring dashboards filter by namespace
- [ ] Write request senders know which namespace to target

## Monitoring Multiple Instances

### MQTT - Subscribe to All Factories

```bash
# All tag changes from all factories
mosquitto_sub -h mqtt.colo.example.com -t "+/+/tags/#"

# Health from all factories
mosquitto_sub -h mqtt.colo.example.com -t "+/+/health"

# Specific factory only
mosquitto_sub -h mqtt.colo.example.com -t "factory-a/#"
```

### Valkey - List All Keys

```bash
# All tag keys
redis-cli -h redis.colo.example.com KEYS "*:tags:*"

# Specific factory
redis-cli -h redis.colo.example.com KEYS "factory-a:*"
```

### Kafka - Consume Multiple Topics

```bash
# All factories (regex)
kafkacat -b kafka.colo.example.com:9092 -t "^factory-.*$" -C

# Specific factory
kafkacat -b kafka.colo.example.com:9092 -t factory-a -C
```

## Troubleshooting

### Write executed on wrong instance

**Symptom:** Write request intended for Factory A was executed on Factory B.

**Cause:** Both instances using the same namespace.

**Fix:** Update configuration to use unique namespaces, restart both instances.

### Write not executed anywhere

**Symptom:** Write request sent but no response received.

**Possible causes:**
1. Wrong namespace in the topic/queue name
2. Wrong namespace in the JSON payload (`topic`, `factory` field)
3. Tag not marked as `writable: true`
4. PLC not connected

**Debug:** Enable debug logging on the target instance:
```bash
warlink --log-debug=mqtt,valkey,kafka
```

### Duplicate tag publications

**Symptom:** Same tag appearing twice in monitoring with different values.

**Cause:** Two instances using the same namespace, publishing from different PLCs.

**Fix:** Update configuration to use unique namespaces.
