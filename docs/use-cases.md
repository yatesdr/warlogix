# Use Case Examples

This guide demonstrates real-world scenarios and how to implement them with WarLink.

## Production Tracking

**Goal:** Capture production data when parts complete and send to MES/ERP systems.

### Scenario

A packaging line has a CompactLogix PLC that sets `PartComplete` true when a box is sealed. You need to capture the serial number, weight, and timestamp for each box and send it to Kafka for downstream processing.

### Implementation

**1. Configure the trigger:**

```yaml
triggers:
  - name: box_complete
    enabled: true
    plc: PackagingPLC
    trigger_tag: Program:Packaging.PartComplete
    condition:
      operator: "=="
      value: true
    ack_tag: Program:Packaging.PartAck
    debounce_ms: 100
    tags:
      - Program:Packaging.SerialNumber
      - Program:Packaging.BoxWeight
      - Program:Packaging.ProductCode
      - Program:Packaging.LineNumber
    kafka_cluster: production
    mqtt_broker: none
    metadata:
      line: packaging-1
      station: sealer
```

**2. PLC logic pattern:**

```
// PLC sets PartComplete when box is sealed
PartComplete := TRUE;

// Wait for acknowledgment from WarLink
IF PartAck = 1 THEN
    // Success - reset for next part
    PartComplete := FALSE;
    PartAck := 0;
ELSIF PartAck = -1 THEN
    // Error - handle failure (alarm, retry, etc.)
    AlarmCode := 100;
    PartComplete := FALSE;
    PartAck := 0;
END_IF;
```

**3. Kafka consumer (Python):**

```python
from kafka import KafkaConsumer
import json

consumer = KafkaConsumer(
    'factory-production-triggers',
    bootstrap_servers=['kafka:9092'],
    value_deserializer=lambda m: json.loads(m.decode('utf-8'))
)

for message in consumer:
    data = message.value
    if data['trigger'] == 'box_complete':
        serial = data['data']['Program:Packaging.SerialNumber']
        weight = data['data']['Program:Packaging.BoxWeight']
        # Send to MES/ERP...
```

---

## Alarm Aggregation

**Goal:** Collect alarms from multiple PLCs and display on a unified dashboard.

### Scenario

A facility has 5 different PLCs (mix of Allen-Bradley, Siemens, and Beckhoff) and needs a single alarm view in Grafana.

### Implementation

**1. Create a TagPack for each PLC's alarms:**

```yaml
tag_packs:
  - name: all_alarms
    enabled: true
    mqtt_enabled: true
    kafka_enabled: false
    valkey_enabled: true
    members:
      - plc: Line1_PLC
        tag: Program:Main.AlarmWord1
      - plc: Line1_PLC
        tag: Program:Main.AlarmWord2
      - plc: Line2_S7
        tag: DB10.0
      - plc: Line2_S7
        tag: DB10.4
      - plc: Packaging_TC
        tag: GVL.AlarmStatus
      - plc: Packaging_TC
        tag: GVL.CriticalFaults
```

**2. Subscribe in Grafana:**

Use the MQTT data source plugin to subscribe to:
```
factory/packs/all_alarms
```

**3. Parse alarm bits in Grafana:**

Create calculated fields to extract individual alarm bits from the alarm words.

---

## Cross-Site Monitoring

**Goal:** Monitor production metrics from multiple factories in a central dashboard.

### Scenario

Three factories need their KPIs visible in a central NOC. Each factory has its own WarLink instance publishing to a shared Kafka cluster.

### Implementation

**1. Configure each factory's WarLink:**

Factory A (`/etc/warlink/config.yaml`):
```yaml
namespace: factory-atlanta
kafka:
  - name: central
    brokers: [kafka.corp.example.com:9092]
    selector: production
    enabled: true
```

Factory B:
```yaml
namespace: factory-boston
kafka:
  - name: central
    brokers: [kafka.corp.example.com:9092]
    selector: production
    enabled: true
```

Factory C:
```yaml
namespace: factory-chicago
kafka:
  - name: central
    brokers: [kafka.corp.example.com:9092]
    selector: production
    enabled: true
```

**2. Topics created:**
- `factory-atlanta-production`
- `factory-boston-production`
- `factory-chicago-production`

**3. Central consumer:**

```python
from kafka import KafkaConsumer

consumer = KafkaConsumer(
    'factory-atlanta-production',
    'factory-boston-production',
    'factory-chicago-production',
    bootstrap_servers=['kafka.corp.example.com:9092']
)

for message in consumer:
    factory = message.topic.split('-')[1]  # atlanta, boston, chicago
    # Route to appropriate dashboard...
```

---

## Recipe Parameter Distribution

**Goal:** Push recipe parameters from MES to PLCs via WarLink write-back.

### Scenario

When a new batch starts, MES needs to update setpoints on multiple PLCs.

### Implementation

**1. Enable write-back on MQTT:**

```yaml
mqtt:
  - name: mes_broker
    broker: mes.example.com
    port: 1883
    enabled: true
```

**2. Mark recipe tags as writable:**

In the Browser tab, mark these tags with `w`:
- `Program:Recipe.Temperature_SP`
- `Program:Recipe.Speed_SP`
- `Program:Recipe.Pressure_SP`

**3. MES publishes recipe:**

```python
import paho.mqtt.client as mqtt
import json

client = mqtt.Client()
client.connect("warlink-host", 1883)

recipe = {
    "topic": "factory",
    "plc": "MixingPLC",
    "tag": "Program:Recipe.Temperature_SP",
    "value": 185.5
}

client.publish("factory/MixingPLC/write", json.dumps(recipe))
```

**4. Monitor write responses:**

```bash
mosquitto_sub -t "factory/MixingPLC/write/response"
```

---

## Energy Monitoring

**Goal:** Track energy consumption across production equipment.

### Scenario

Power meters connected to PLCs report kWh values. Need to log these to a time-series database.

### Implementation

**1. Configure energy tags:**

```yaml
plcs:
  - name: PowerMeter1
    address: 192.168.1.50
    family: s7
    tags:
      - name: DB100.0
        alias: kWh_Line1
        data_type: REAL
        enabled: true
      - name: DB100.4
        alias: kW_Line1
        data_type: REAL
        enabled: true
```

**2. Publish to Valkey with TTL:**

```yaml
valkey:
  - name: timeseries
    address: redis:6379
    key_ttl: 86400s  # 24 hour retention
    publish_changes: true
    enabled: true
```

**3. Consume in TimescaleDB via Redis Streams:**

Or use Kafka with a Kafka Connect sink to TimescaleDB/InfluxDB.

---

## Quality Data Capture

**Goal:** Capture quality measurements at inspection stations with guaranteed delivery.

### Scenario

Vision system results need to be captured with the part's serial number and sent to a quality database with exactly-once semantics.

### Implementation

**1. Configure trigger with QoS 2:**

```yaml
triggers:
  - name: inspection_complete
    enabled: true
    plc: VisionPLC
    trigger_tag: InspectionDone
    condition:
      operator: "=="
      value: true
    ack_tag: InspectionAck
    tags:
      - SerialNumber
      - PassFail
      - Measurement1
      - Measurement2
      - DefectCode
    mqtt_broker: quality_broker  # Will use QoS 2
    kafka_cluster: none
    selector: quality-data
    metadata:
      station: inspection-1
      type: vision
```

**2. MQTT subscriber with QoS 2:**

```python
import paho.mqtt.client as mqtt

def on_message(client, userdata, msg):
    # Guaranteed exactly-once delivery
    data = json.loads(msg.payload)
    insert_to_quality_db(data)

client = mqtt.Client()
client.on_message = on_message
client.connect("broker", 1883)
client.subscribe("factory/quality-data/triggers/inspection_complete", qos=2)
client.loop_forever()
```

---

## Machine State Dashboards

**Goal:** Real-time OEE dashboard showing machine states.

### Scenario

Track Running/Stopped/Faulted state for 20 machines and calculate OEE in real-time.

### Implementation

**1. Create TagPack per production line:**

```yaml
tag_packs:
  - name: line1_oee
    enabled: true
    mqtt_enabled: true
    valkey_enabled: true
    members:
      - plc: Line1_PLC
        tag: Machine1_State
      - plc: Line1_PLC
        tag: Machine1_CycleCount
      - plc: Line1_PLC
        tag: Machine1_GoodParts
      - plc: Line1_PLC
        tag: Machine1_RejectParts
        ignore_changes: true  # Don't trigger on rejects alone
      - plc: Line1_PLC
        tag: Machine2_State
      # ... more machines
```

**2. Valkey for current state, Kafka for history:**

```yaml
valkey:
  - name: dashboard
    address: redis:6379
    publish_changes: true
    enabled: true

kafka:
  - name: historian
    brokers: [kafka:9092]
    publish_changes: true
    enabled: true
```

**3. Grafana dashboard:**

- Use Redis data source for real-time gauges
- Use Kafka/ClickHouse for historical OEE trends

---

## Predictive Maintenance Data Collection

**Goal:** Collect vibration and temperature data for ML-based predictive maintenance.

### Scenario

High-frequency sensor data from bearings needs to be captured for training predictive models.

### Implementation

**1. Configure fast polling for sensor PLC:**

```yaml
plcs:
  - name: SensorPLC
    address: 192.168.1.60
    family: beckhoff
    poll_rate: 100ms  # Fast polling for vibration data
    tags:
      - name: MAIN.Bearing1_Vibration
        enabled: true
      - name: MAIN.Bearing1_Temp
        enabled: true
      - name: MAIN.Motor1_Current
        enabled: true
```

**2. Stream to Kafka for ML pipeline:**

```yaml
kafka:
  - name: ml_pipeline
    brokers: [kafka:9092]
    selector: sensor-data
    publish_changes: true
    enabled: true
```

**3. Kafka Streams or Flink for feature engineering:**

Process raw sensor data into features for ML models.

---

## Integration Patterns Summary

| Use Case | Primary Service | Secondary | Key Features |
|----------|-----------------|-----------|--------------|
| Production Tracking | Kafka | - | Triggers, Ack tags |
| Alarm Aggregation | MQTT, Valkey | - | TagPacks, Retained messages |
| Cross-Site Monitoring | Kafka | - | Namespaces, Selectors |
| Recipe Distribution | MQTT | - | Write-back |
| Energy Monitoring | Valkey | Kafka | TTL, Time-series |
| Quality Capture | MQTT (QoS 2) | - | Triggers, Exactly-once |
| OEE Dashboards | Valkey | Kafka | TagPacks, Real-time |
| Predictive Maintenance | Kafka | - | Fast polling, Streaming |
