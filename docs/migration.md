# Migration Guide

This guide helps you migrate from other PLC gateway solutions to WarLink.

## From Kepware/KEPServerEX

### Conceptual Mapping

| Kepware Concept | WarLink Equivalent |
|-----------------|---------------------|
| Channel | PLC connection |
| Device | PLC configuration |
| Tag Group | Not needed (flat structure) |
| Tag | Tag in Browser |
| OPC UA Server | REST API |
| IoT Gateway | MQTT/Kafka publishers |
| Advanced Tags | TagPacks |

### Migration Steps

**1. Export Kepware tags:**

Export your tag configuration from Kepware to CSV or use the configuration API.

**2. Map channels to PLCs:**

Kepware channel:
```
Channel: Allen-Bradley
  Driver: Allen-Bradley Micro800 Ethernet
  Device: PackagingPLC
    IP: 192.168.1.100
```

WarLink equivalent:
```yaml
plcs:
  - name: PackagingPLC
    address: 192.168.1.100
    family: micro800
    enabled: true
```

**3. Configure tags:**

For Allen-Bradley/Beckhoff: Tags are auto-discovered. Enable desired tags in the Browser tab.

For S7/Omron FINS: Add tags manually with addresses matching your Kepware configuration.

**4. Replace IoT Gateway:**

Kepware MQTT Agent:
```
Broker: mqtt.example.com:1883
Topic: kepware/{channel}/{device}/{tag}
```

WarLink:
```yaml
namespace: factory
mqtt:
  - name: production
    broker: mqtt.example.com
    port: 1883
    enabled: true
# Topics: factory/{plc}/tags/{tag}
```

### Key Differences

| Feature | Kepware | WarLink |
|---------|---------|----------|
| Licensing | Per-driver, per-connection | Free/Open source |
| Tag hierarchy | Channels → Devices → Groups | Flat (PLC → Tags) |
| OPC UA | Native server | Not supported (use REST) |
| Tag addressing | Abstracted | Native PLC addresses |
| Configuration | GUI + runtime API | TUI + YAML file |

---

## From Ignition Edge

### Conceptual Mapping

| Ignition Concept | WarLink Equivalent |
|------------------|---------------------|
| Device Connection | PLC configuration |
| Tag Provider | PLC in Browser |
| OPC Tag | Tag |
| UDT Definition | Auto-discovered UDT |
| Tag Historian | Kafka (with sink) |
| MQTT Transmission | MQTT publisher |
| Tag Groups | TagPacks |

### Migration Steps

**1. Export Ignition tag configuration:**

Use Ignition's tag export feature to get your tag structure.

**2. Map device connections:**

Ignition:
```
Device: Line1_PLC
Type: Allen-Bradley Logix Driver
Hostname: 192.168.1.100
Slot: 0
```

WarLink:
```yaml
plcs:
  - name: Line1_PLC
    address: 192.168.1.100
    family: logix
    slot: 0
    enabled: true
```

**3. Replace MQTT Transmission:**

Ignition MQTT Transmission publishes in Sparkplug B format by default. WarLink uses a simpler JSON format.

Ignition topic:
```
spBv1.0/MyGroup/DDATA/Line1_PLC
```

WarLink topic:
```
factory/Line1_PLC/tags/Counter
```

**4. Update consumers:**

Consumers expecting Sparkplug B format will need to be updated for WarLink's JSON format.

### Key Differences

| Feature | Ignition Edge | WarLink |
|---------|---------------|----------|
| Licensing | Per-device or unlimited | Free/Open source |
| Scripting | Python scripting | Not supported |
| Historian | Built-in | External (Kafka/DB) |
| MQTT format | Sparkplug B | Simple JSON |
| UI | Web-based | Terminal (TUI) |
| Tag expressions | Supported | Not supported |

---

## From Node-RED with PLC Nodes

### Conceptual Mapping

| Node-RED Concept | WarLink Equivalent |
|------------------|---------------------|
| PLC node (contrib) | Built-in driver |
| Flow | Not needed |
| Function node | Not supported |
| MQTT out node | MQTT publisher |
| Change node | Change detection (automatic) |
| Inject node (polling) | Poll rate config |

### Migration Steps

**1. Identify PLC connections:**

Find all PLC nodes in your flows:
```
[s7 read] → [function] → [mqtt out]
```

**2. Configure equivalent PLCs:**

Node-RED s7 node:
```json
{
  "address": "192.168.1.100",
  "rack": 0,
  "slot": 1,
  "localtsap": "0x0100",
  "remotetsap": "0x0200"
}
```

WarLink:
```yaml
plcs:
  - name: S7_PLC
    address: 192.168.1.100
    family: s7
    slot: 1  # For S7-1200/1500 use 0
    enabled: true
    tags:
      - name: DB1.0
        data_type: DINT
        enabled: true
```

**3. Replace function nodes:**

If you have transformation logic in function nodes, you'll need to handle it in your consumers.

WarLink publishes raw values; transformations happen downstream.

### Key Differences

| Feature | Node-RED | WarLink |
|---------|----------|----------|
| Transformation | Function nodes | Not supported |
| Visual programming | Flow-based | Configuration-based |
| PLC support | Via contrib nodes | Built-in, optimized |
| Performance | Single-threaded | Multi-threaded |
| Batching | Manual | Automatic |

---

## From Custom Solutions

### Common Patterns to Replace

**Pattern 1: Python polling script**

Before:
```python
while True:
    value = plc.read("Counter")
    mqtt_client.publish(f"plc/{tag}", json.dumps({"value": value}))
    time.sleep(0.5)
```

After: Configure PLC and enable tag in WarLink.

**Pattern 2: OPC UA client**

Before: Custom OPC UA client reading from Kepware/other server.

After: Direct PLC connection with WarLink (eliminates OPC server).

**Pattern 3: Protocol-specific scripts**

Before: Multiple scripts for different PLCs (pylogix, python-snap7, etc.)

After: Single WarLink instance with multi-vendor support.

### Benefits of Migration

| Aspect | Custom Solution | WarLink |
|--------|-----------------|----------|
| Maintenance | High (custom code) | Low (configuration) |
| Performance | Variable | Optimized batching |
| Multi-vendor | Multiple libraries | Single binary |
| Monitoring | Custom | Built-in health checks |
| Recovery | Manual restart | Auto-reconnect |

---

## Data Format Migration

### Topic/Key Structure

Adapt consumers to WarLink's namespace-based structure:

| Old Pattern | WarLink Pattern |
|-------------|------------------|
| `plc/tags/{tag}` | `{namespace}/{plc}/tags/{tag}` |
| `factory/line1/{tag}` | `{namespace}/{plc}/tags/{tag}` |
| `{plc}/{tag}` | `{namespace}/{plc}/tags/{tag}` |

### JSON Payload

WarLink uses consistent JSON format across services:

```json
{
  "plc": "Line1_PLC",
  "tag": "Counter",
  "value": 42,
  "type": "DINT",
  "writable": false,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

**Adapting consumers:**

```python
# Old format
value = payload["value"]

# WarLink format (same field name, more metadata)
value = payload["value"]
plc = payload["plc"]
tag = payload["tag"]
data_type = payload["type"]
```

---

## Parallel Operation

During migration, you can run both systems in parallel:

```
┌─────────┐     ┌─────────────┐     ┌──────────┐
│   PLC   │────▶│  Kepware    │────▶│ Consumer │ (existing)
└─────────┘     └─────────────┘     └──────────┘
     │
     │          ┌─────────────┐     ┌──────────┐
     └─────────▶│  WarLink   │────▶│ Consumer │ (new)
                └─────────────┘     └──────────┘
```

**Important:** Some PLCs limit concurrent connections. Check:
- Allen-Bradley: Usually supports multiple connections
- Siemens S7: Check PUT/GET connection limits
- Beckhoff: May require multiple routes
- Omron: Check connection limits in PLC settings

### Migration Checklist

- [ ] Inventory existing tags and data flows
- [ ] Map PLC connections to WarLink config
- [ ] Configure namespace and selectors
- [ ] Test with subset of tags
- [ ] Update consumers for new format
- [ ] Run parallel for validation
- [ ] Monitor data quality
- [ ] Cutover consumers
- [ ] Decommission old solution

---

## Rollback Plan

If issues arise:

1. **Immediate rollback:** Restart old solution, disable WarLink
2. **Consumer rollback:** Point consumers back to old topics
3. **Hybrid operation:** Run both systems with different consumers

Keep your old configuration and scripts until migration is validated.
