# Architecture Overview

This document describes WarLink's internal architecture and data flow.

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              WarLink                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐    │
│  │                         PLC Manager                                  │    │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐       │    │
│  │  │  Logix  │ │  S7     │ │  ADS    │ │  FINS   │ │  EIP    │       │    │
│  │  │ Driver  │ │ Driver  │ │ Driver  │ │ Driver  │ │ Driver  │       │    │
│  │  └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘       │    │
│  │       │           │           │           │           │             │    │
│  │       └───────────┴─────┬─────┴───────────┴───────────┘             │    │
│  │                         │                                            │    │
│  │                   Tag Cache                                          │    │
│  │              (Change Detection)                                      │    │
│  └─────────────────────────┬───────────────────────────────────────────┘    │
│                            │                                                 │
│  ┌─────────────────────────┼───────────────────────────────────────────┐    │
│  │                    Publish Engine                                    │    │
│  │       ┌─────────────────┼─────────────────┐                         │    │
│  │       │                 │                 │                         │    │
│  │  ┌────▼────┐ ┌─────▼─────┐ ┌────▼────┐ ┌────▼────┐                │    │
│  │  │ TagPack │ │  Trigger  │ │  Push   │ │  REST   │                │    │
│  │  │ Manager │ │  Manager  │ │ Manager │ │  API    │                │    │
│  │  └────┬────┘ └─────┬─────┘ └────┬────┘ └────┬────┘                │    │
│  │       │            │            │           │                      │    │
│  └───────┼────────────┼────────────┼───────────┼─────────────────────┘    │
│          │                 │                │                                │
│  ┌───────┼─────────────────┼────────────────┼──────────────────────────┐    │
│  │       │           Service Publishers     │                          │    │
│  │  ┌────▼────┐      ┌─────▼─────┐     ┌────▼────┐                    │    │
│  │  │  MQTT   │      │   Kafka   │     │  Valkey │                    │    │
│  │  │Publisher│      │  Manager  │     │ Manager │                    │    │
│  │  └────┬────┘      └─────┬─────┘     └────┬────┘                    │    │
│  └───────┼─────────────────┼────────────────┼──────────────────────────┘    │
│          │                 │                │                                │
└──────────┼─────────────────┼────────────────┼────────────────────────────────┘
           │                 │                │
           ▼                 ▼                ▼
      MQTT Brokers     Kafka Clusters    Valkey/Redis
```

## Data Flow

### Read Path (PLC → Brokers)

```
┌──────┐    Poll     ┌─────────┐   Change    ┌──────────┐   Publish   ┌─────────┐
│ PLC  │───────────▶│  Cache  │─────────────▶│ Publisher│────────────▶│ Broker  │
└──────┘            └─────────┘  Detected    └──────────┘             └─────────┘
                         │
                         │ No Change
                         ▼
                    (Skip Publish)
```

1. **Poll**: Driver reads tags from PLC at configured poll rate
2. **Cache**: Values stored in tag cache with change detection
3. **Change Detection**: Compare new values against cached values
4. **Publish**: If changed, serialize to JSON and publish to enabled services

### Write Path (Brokers → PLC)

```
┌─────────┐  Subscribe  ┌──────────┐  Validate  ┌─────────┐   Write    ┌──────┐
│ Broker  │────────────▶│ Consumer │───────────▶│ Driver  │───────────▶│ PLC  │
└─────────┘             └──────────┘            └─────────┘            └──────┘
                              │
                              │ Response
                              ▼
                        ┌──────────┐
                        │ Publisher│──────▶ Response Topic/Channel
                        └──────────┘
```

1. **Subscribe**: WarLink consumes from write topic/queue
2. **Validate**: Check namespace match, tag exists, tag is writable
3. **Write**: Send value to PLC via appropriate driver
4. **Response**: Publish success/failure to response topic

---

## Component Details

### PLC Drivers

Each PLC family has a dedicated driver implementing a common interface:

| Driver | Protocol | Port | Batching |
|--------|----------|------|----------|
| Logix | EtherNet/IP (CIP) | 44818 | Multiple Service Packet |
| Micro800 | EtherNet/IP (CIP) | 44818 | Multiple Service Packet |
| S7 | S7comm (ISO-on-TCP) | 102 | PDU batching |
| ADS | TwinCAT ADS | 48898 | SumUp Read |
| FINS | Omron FINS | 9600 | Multi-area read |
| EIP (Omron) | EtherNet/IP (CIP) | 44818 | Multiple Service Packet |

### Tag Cache

The tag cache stores current values and tracks changes:

```go
type TagCache struct {
    PLCName     string
    TagName     string
    Value       interface{}
    Type        string
    LastChanged time.Time
    IgnoreList  []string  // UDT members to ignore for change detection
}
```

Change detection compares serialized values to handle complex types (arrays, UDTs).

### Namespace Builder

Constructs consistent topic/key names across services:

| Service | Pattern | Example |
|---------|---------|---------|
| MQTT | `{ns}[/{sel}]/{plc}/tags/{tag}` | `factory/line1/PLC1/tags/Counter` |
| Kafka | `{ns}[-{sel}]` | `factory-line1` |
| Valkey | `{ns}[:{sel}]:{plc}:tags:{tag}` | `factory:line1:PLC1:tags:Counter` |

---

## TagPack Architecture

```
┌────────────────────────────────────────────────────────────┐
│                    TagPack Manager                         │
│  ┌──────────────────────────────────────────────────────┐ │
│  │ Pack: "production_metrics"                            │ │
│  │  Members:                                             │ │
│  │   - PLC1.Counter (trigger)                           │ │
│  │   - PLC1.Speed (trigger)                             │ │
│  │   - PLC1.Timestamp (ignore)                          │ │
│  │   - PLC2.Temperature (trigger)                       │ │
│  └──────────────────────────────────────────────────────┘ │
│                          │                                 │
│                    Tag Change                              │
│                    (non-ignored)                           │
│                          │                                 │
│                          ▼                                 │
│                   ┌──────────────┐                        │
│                   │  Debounce    │  250ms                 │
│                   │    Timer     │                        │
│                   └──────────────┘                        │
│                          │                                 │
│                    Timer Expires                           │
│                          │                                 │
│                          ▼                                 │
│              ┌─────────────────────┐                      │
│              │ Collect All Members │                      │
│              │   (atomic read)     │                      │
│              └─────────────────────┘                      │
│                          │                                 │
│                          ▼                                 │
│              ┌─────────────────────┐                      │
│              │  Publish to MQTT,   │                      │
│              │  Kafka, Valkey      │                      │
│              └─────────────────────┘                      │
└────────────────────────────────────────────────────────────┘
```

### Debounce Behavior

1. First trigger starts 250ms timer
2. Additional triggers during window are absorbed
3. After 250ms, all member values collected
4. Single message published with all current values

---

## Trigger Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                      Trigger Engine                             │
│                                                                 │
│  ┌───────────────┐                                             │
│  │ Monitor Loop  │◀──────── 100ms poll interval               │
│  │ (per trigger) │                                             │
│  └───────┬───────┘                                             │
│          │                                                      │
│          │ Read trigger tag                                     │
│          ▼                                                      │
│  ┌───────────────┐                                             │
│  │   Condition   │                                             │
│  │   Evaluator   │                                             │
│  └───────┬───────┘                                             │
│          │                                                      │
│          │ Rising edge detected (false → true)                 │
│          ▼                                                      │
│  ┌───────────────┐        ┌──────────────┐                    │
│  │   Debounce    │───────▶│  Read Data   │                    │
│  │    Check      │        │    Tags      │                    │
│  └───────────────┘        └──────┬───────┘                    │
│                                  │                              │
│                                  │ Collect pack data (if any)  │
│                                  ▼                              │
│                          ┌──────────────┐                      │
│                          │   Serialize  │                      │
│                          │     JSON     │                      │
│                          └──────┬───────┘                      │
│                                 │                               │
│              ┌──────────────────┼──────────────────┐           │
│              ▼                  ▼                  ▼           │
│        ┌──────────┐      ┌──────────┐      ┌──────────┐       │
│        │   MQTT   │      │  Kafka   │      │   Ack    │       │
│        │  QoS 2   │      │ Produce  │      │  Write   │       │
│        └──────────┘      └──────────┘      └──────────┘       │
│                                                                 │
│  State Machine:                                                 │
│  ┌──────────┐    trigger    ┌──────────┐   condition   ┌──────┐│
│  │  Armed   │──────────────▶│  Firing  │──────────────▶│Cooldown│
│  └──────────┘               └──────────┘    false      └───┬───┘│
│       ▲                                                    │    │
│       └────────────────────────────────────────────────────┘    │
│                        condition false                          │
└────────────────────────────────────────────────────────────────┘
```

### Trigger States

| State | Description | Transitions |
|-------|-------------|-------------|
| Disabled | Trigger not running | → Armed (Start) |
| Armed | Monitoring for condition | → Firing (condition met) |
| Firing | Capturing and publishing | → Cooldown (complete) |
| Cooldown | Waiting for condition reset | → Armed (condition false) |
| Error | Publish failed | → Cooldown (auto), Armed (Reset) |

---

## Service Publishers

### MQTT Publisher

```
┌──────────────────────────────────────────────────────┐
│                  MQTT Publisher                       │
│                                                       │
│  ┌─────────────┐     ┌─────────────┐                │
│  │   Paho      │     │   Topic     │                │
│  │   Client    │     │   Builder   │                │
│  └──────┬──────┘     └──────┬──────┘                │
│         │                   │                        │
│         │    ┌──────────────┴──────────────┐        │
│         │    │      Message Queue          │        │
│         │    │  (buffered, async publish)  │        │
│         │    └──────────────┬──────────────┘        │
│         │                   │                        │
│         └───────────────────┘                        │
│                    │                                 │
│                    ▼                                 │
│  QoS Levels:                                        │
│   - Tags/Health: QoS 1 (at least once)             │
│   - Triggers: QoS 2 (exactly once)                 │
│   - TagPacks: QoS 1, retained                      │
└──────────────────────────────────────────────────────┘
```

### Kafka Manager

```
┌──────────────────────────────────────────────────────┐
│                   Kafka Manager                       │
│                                                       │
│  ┌─────────────────────────────────────────────────┐ │
│  │              Per-Cluster Writers                 │ │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐        │ │
│  │  │Cluster 1│  │Cluster 2│  │Cluster 3│        │ │
│  │  └────┬────┘  └────┬────┘  └────┬────┘        │ │
│  │       │            │            │              │ │
│  │  ┌────▼────────────▼────────────▼────┐        │ │
│  │  │         Batch Accumulator         │        │ │
│  │  │  (100 msgs or 20ms, whichever first)│       │ │
│  │  └────────────────┬──────────────────┘        │ │
│  │                   │                            │ │
│  │                   ▼                            │ │
│  │           kafka-go Writer                      │ │
│  │    (additional internal batching)              │ │
│  └─────────────────────────────────────────────────┘ │
│                                                       │
│  Message Keys:                                       │
│   - Tags: {plc}.{tag}                               │
│   - Packs: pack:{packname}                          │
│   - Triggers: {trigger-name}                        │
│   - Health: {plc}                                   │
└──────────────────────────────────────────────────────┘
```

### Valkey Manager

```
┌──────────────────────────────────────────────────────┐
│                   Valkey Manager                      │
│                                                       │
│  ┌─────────────────────────────────────────────────┐ │
│  │              Per-Server Clients                  │ │
│  │  ┌─────────┐  ┌─────────┐                      │ │
│  │  │Server 1 │  │Server 2 │                      │ │
│  │  └────┬────┘  └────┬────┘                      │ │
│  │       │            │                            │ │
│  │  ┌────▼────────────▼────┐                      │ │
│  │  │    go-redis Client   │                      │ │
│  │  │   (connection pool)  │                      │ │
│  │  └──────────┬───────────┘                      │ │
│  │             │                                   │ │
│  │  ┌──────────┴───────────────────────┐          │ │
│  │  │  SET (key storage)               │          │ │
│  │  │  PUBLISH (change notifications)  │          │ │
│  │  │  BLPOP (write-back queue)        │          │ │
│  │  └──────────────────────────────────┘          │ │
│  └─────────────────────────────────────────────────┘ │
│                                                       │
│  Key Format: {ns}:{sel}:{plc}:tags:{tag}            │
│  Channel Format: {ns}:{sel}:{plc}:changes           │
└──────────────────────────────────────────────────────┘
```

---

## Threading Model

```
Main Goroutine
    │
    ├── TUI Event Loop (local mode)
    │   or
    ├── SSH Server (daemon mode)
    │   ├── Session 1 TUI (independent per connection)
    │   ├── Session 2 TUI
    │   └── Session N TUI
    │
    ├── PLC Manager (shared)
    │   ├── PLC1 Poll Loop (goroutine per PLC)
    │   ├── PLC2 Poll Loop
    │   └── PLC3 Poll Loop
    │
    ├── TagPack Manager
    │   └── Debounce Loop (single goroutine)
    │
    ├── Trigger Manager
    │   ├── Trigger1 Monitor Loop (goroutine per trigger)
    │   ├── Trigger2 Monitor Loop
    │   └── Trigger3 Monitor Loop
    │
    ├── Push Manager
    │   ├── Push1 Monitor Loop (goroutine per push)
    │   └── Push2 Monitor Loop
    │
    ├── MQTT Publishers
    │   ├── Broker1 Client (goroutine per broker)
    │   └── Broker2 Client
    │
    ├── Kafka Manager
    │   ├── Cluster1 Writer (goroutine per cluster)
    │   └── Cluster1 Consumer (if writeback enabled)
    │
    ├── Valkey Manager
    │   ├── Server1 Client
    │   └── Server1 Writeback Loop (if enabled)
    │
    ├── Web Server (REST API + Browser UI + SSE)
    │
    └── Health Publisher (periodic timer)
```

---

## Memory Management

### Tag Value Storage

- Primitive types: Stored directly
- Arrays: Stored as Go slices
- UDTs: Stored as `map[string]interface{}`
- Strings: Stored as Go strings (UTF-8)

### Buffer Management

- PLC read buffers: Pooled per driver
- JSON serialization: Allocated per publish
- Kafka batches: Reused per flush cycle

### Resource Limits

| Resource | Default Limit | Configurable |
|----------|---------------|--------------|
| PLC connections | Unlimited | By config |
| MQTT queue depth | 1000 messages | No |
| Kafka batch size | 100 messages | No |
| HTTP connections | 100 concurrent | No |
