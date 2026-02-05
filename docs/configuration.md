# Configuration Reference

WarLogix uses a YAML configuration file stored at `~/.warlogix/config.yaml` by default. Use `-config /path/to/file` to specify an alternate location.

## Complete Example

```yaml
plcs:
  # Allen-Bradley ControlLogix/CompactLogix (automatic tag discovery)
  - name: MainPLC
    address: 192.168.1.100
    family: logix
    slot: 0
    enabled: true                    # Auto-connect on startup
    health_check_enabled: true       # Publish health status (default: true)
    poll_rate: 500ms                 # Per-PLC poll rate (overrides global)
    tags:
      - name: Program:MainProgram.Counter
        enabled: true
        writable: true
      - name: Program:MainProgram.MachineStatus
        enabled: true
        ignore_changes: [Timestamp, HeartbeatCount]  # UDT members to ignore

  # Allen-Bradley Micro800
  - name: Micro850
    address: 192.168.1.101
    family: micro800
    slot: 0
    enabled: true

  # Siemens S7 (manual tags with byte offsets)
  - name: SiemensPLC
    address: 192.168.1.102
    family: s7
    slot: 1                          # Rack/slot (0 for S7-1200/1500)
    enabled: true
    health_check_enabled: false      # Disable health publishing
    tags:
      - name: DB1.0
        alias: ProductCount          # Friendly name for publishing
        data_type: DINT
        enabled: true
      - name: DB1.4
        alias: Temperature
        data_type: REAL
        enabled: true
        writable: true

  # Beckhoff TwinCAT (automatic symbol discovery)
  - name: TwinCAT
    address: 192.168.1.103
    family: beckhoff
    ams_net_id: 192.168.1.103.1.1    # PLC's AMS Net ID
    ams_port: 851                     # 851 for TC3, 801 for TC2
    enabled: true
    tags:
      - name: MAIN.Temperature
        enabled: true
      - name: GVL.GlobalCounter
        enabled: true
        writable: true

  # Omron FINS (manual tags with memory areas)
  - name: OmronPLC
    address: 192.168.1.104
    family: omron
    fins_port: 9600                   # UDP port (default: 9600)
    fins_node: 0                      # FINS node number
    fins_network: 0                   # FINS network number
    fins_unit: 0                      # CPU unit number
    enabled: true
    tags:
      - name: DM100
        alias: MotorSpeed
        data_type: DINT
        enabled: true
      - name: DM104
        alias: SetPoint
        data_type: REAL
        enabled: true
        writable: true

rest:
  enabled: true
  port: 8080
  host: 0.0.0.0

mqtt:
  - name: LocalBroker
    enabled: true
    broker: localhost
    port: 1883
    client_id: warlogix-main
    root_topic: factory
    # username: user              # Optional authentication
    # password: pass
    # use_tls: true               # Enable TLS

valkey:
  - name: LocalValkey
    enabled: true
    address: localhost:6379
    database: 0
    factory: factory                  # Key prefix
    # password: secret              # Optional authentication
    # use_tls: true
    key_ttl: 60s                      # Key expiration (0 = no expiry)
    publish_changes: true             # Pub/Sub on changes
    enable_writeback: true            # Enable write-back queue

kafka:
  - name: LocalKafka
    enabled: true
    brokers: [localhost:9092]
    topic: plc-tags                   # Topic for tag changes
    publish_changes: true             # Publish tag changes
    # use_tls: true
    # sasl_mechanism: PLAIN          # PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
    # username: user
    # password: pass
    required_acks: -1                 # -1=all, 0=none, 1=leader
    max_retries: 3
    retry_backoff: 100ms

triggers:
  - name: ProductComplete
    enabled: true
    plc: MainPLC
    trigger_tag: Program:MainProgram.ProductReady
    condition:
      operator: "=="                  # ==, !=, >, <, >=, <=
      value: true
    ack_tag: Program:MainProgram.ProductAck  # Optional acknowledgment
    debounce_ms: 100                  # Debounce time
    tags:                             # Tags to capture
      - ProductID
      - BatchNumber
      - Quantity
    kafka_cluster: LocalKafka
    topic: production-events
    metadata:                         # Static metadata
      line: Line1
      station: Assembly

poll_rate: 1s                         # Global default poll rate
```

## PLC Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier for the PLC |
| `address` | string | Yes | IP address or hostname |
| `family` | string | Yes | `logix`, `micro800`, `s7`, `beckhoff`, `omron` |
| `slot` | int | No | CPU slot number (default: 0) |
| `enabled` | bool | No | Auto-connect on startup (default: false) |
| `health_check_enabled` | bool | No | Publish health status (default: true) |
| `poll_rate` | duration | No | Per-PLC poll rate (overrides global) |
| `tags` | list | No | Tags to publish |

### Beckhoff-specific Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ams_net_id` | string | Yes | PLC's AMS Net ID (e.g., `192.168.1.100.1.1`) |
| `ams_port` | int | No | AMS port (default: 851 for TwinCAT 3) |

### Omron-specific Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `fins_port` | int | No | FINS UDP port (default: 9600) |
| `fins_network` | int | No | FINS network number (default: 0) |
| `fins_node` | int | No | FINS node number (default: 0) |
| `fins_unit` | int | No | CPU unit number (default: 0) |

## Tag Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Tag name or address |
| `alias` | string | No | Friendly name for publishing |
| `data_type` | string | No | Data type for manual tags (S7/Omron) |
| `enabled` | bool | No | Enable publishing (default: false) |
| `writable` | bool | No | Allow write operations (default: false) |
| `ignore_changes` | list | No | UDT member names to ignore for change detection |

## REST Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | No | Enable REST API (default: false) |
| `port` | int | No | HTTP port (default: 8080) |
| `host` | string | No | Bind address (default: 0.0.0.0) |

## MQTT Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier |
| `enabled` | bool | No | Enable this broker (default: false) |
| `broker` | string | Yes | Broker hostname or IP |
| `port` | int | No | Broker port (default: 1883) |
| `client_id` | string | Yes | MQTT client ID |
| `root_topic` | string | Yes | Root topic for all messages |
| `username` | string | No | Authentication username |
| `password` | string | No | Authentication password |
| `use_tls` | bool | No | Enable TLS (default: false) |

## Valkey/Redis Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier |
| `enabled` | bool | No | Enable this server (default: false) |
| `address` | string | Yes | Server address (host:port) |
| `database` | int | No | Redis database number (default: 0) |
| `factory` | string | Yes | Key prefix for all keys |
| `password` | string | No | Authentication password |
| `use_tls` | bool | No | Enable TLS (default: false) |
| `key_ttl` | duration | No | Key expiration time (0 = no expiry) |
| `publish_changes` | bool | No | Publish to Pub/Sub on changes |
| `enable_writeback` | bool | No | Enable write-back queue |

## Kafka Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier |
| `enabled` | bool | No | Enable this cluster (default: false) |
| `brokers` | list | Yes | List of broker addresses |
| `topic` | string | No | Topic for tag change publishing |
| `publish_changes` | bool | No | Publish tag changes |
| `use_tls` | bool | No | Enable TLS (default: false) |
| `tls_skip_verify` | bool | No | Skip TLS certificate verification |
| `sasl_mechanism` | string | No | SASL auth: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512 |
| `username` | string | No | SASL username |
| `password` | string | No | SASL password |
| `required_acks` | int | No | Acks required: -1=all, 0=none, 1=leader |
| `max_retries` | int | No | Max publish retries |
| `retry_backoff` | duration | No | Retry backoff interval |

## Trigger Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier |
| `enabled` | bool | No | Enable this trigger (default: false) |
| `plc` | string | Yes | PLC name to monitor |
| `trigger_tag` | string | Yes | Tag to watch for condition |
| `condition` | object | Yes | Condition to fire trigger |
| `ack_tag` | string | No | Tag to write acknowledgment |
| `debounce_ms` | int | No | Debounce time in milliseconds |
| `tags` | list | Yes | Tags to capture when triggered |
| `kafka_cluster` | string | Yes | Kafka cluster name |
| `topic` | string | Yes | Kafka topic for events |
| `metadata` | map | No | Static metadata to include |

### Condition Object

| Field | Type | Description |
|-------|------|-------------|
| `operator` | string | Comparison: `==`, `!=`, `>`, `<`, `>=`, `<=` |
| `value` | any | Value to compare against |

## Duration Format

Duration values accept Go-style duration strings:
- `500ms` - 500 milliseconds
- `1s` - 1 second
- `1m30s` - 1 minute 30 seconds
- `1h` - 1 hour
