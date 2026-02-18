# Configuration Reference

In normal use the config file will not need to be edited by hand, but for pre-configuring automation or BigFix/Ansible deployments it is useful to document the format.  

WarLink uses a YAML configuration file stored at `~/.warlink/config.yaml` by default. Use `-config /path/to/file` to specify an alternate location.

## Complete Example

```yaml
plcs:
  # Allen-Bradley ControlLogix/CompactLogix (automatic tag discovery)
  - name: MainPLC
    address: 192.168.1.100
    family: logix
    slot: 0
    # connection_path: "1,0"         # CIP route (default: 1,{slot}). Only set for multi-hop routing.
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

  # Allen-Bradley SLC-500 (experimental, manual tags with data table addresses)
  - name: SLC500
    address: 192.168.1.50
    family: slc500
    enabled: true
    tags:
      - name: "N7:0"
        alias: ProductCount
        data_type: INT
        enabled: true

web:
  enabled: true
  host: 0.0.0.0
  port: 8080
  api:
    enabled: true
  ui:
    enabled: true
    users:
      - username: admin
        password_hash: "$2a$10$..."    # bcrypt hash (managed via UI or CLI)
        role: admin

mqtt:
  - name: LocalBroker
    enabled: true
    broker: localhost
    port: 1883
    client_id: warlink-main
    selector: line1                   # Optional sub-namespace
    # username: user              # Optional authentication
    # password: pass
    # use_tls: true               # Enable TLS

valkey:
  - name: LocalValkey
    enabled: true
    address: localhost:6379
    database: 0
    selector: line1                   # Optional sub-namespace
    # password: secret              # Optional authentication
    # use_tls: true
    key_ttl: 60s                      # Key expiration (0 = no expiry)
    publish_changes: true             # Pub/Sub on changes
    enable_writeback: true            # Enable write-back queue

kafka:
  - name: LocalKafka
    enabled: true
    brokers: [localhost:9092]
    publish_changes: true             # Publish tag changes
    # use_tls: true
    # sasl_mechanism: PLAIN          # PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
    # username: user
    # password: pass
    required_acks: -1                 # -1=all, 0=none, 1=leader
    max_retries: 3
    retry_backoff: 100ms

rules:
  - name: ProductComplete
    enabled: true
    conditions:
      - plc: MainPLC
        tag: Program:MainProgram.ProductReady
        operator: "=="
        value: true
    logic_mode: and                   # "and" (default) or "or"
    debounce_ms: 100
    cooldown_ms: 1000
    actions:
      - type: publish
        tag_or_pack: pack:ProductionMetrics
        include_trigger: true
        kafka_cluster: LocalKafka
        kafka_topic: production-events
      - type: writeback
        write_tag: Program:MainProgram.ProductAck
        write_value: 1
    cleared_actions:
      - type: writeback
        write_tag: Program:MainProgram.ProductAck
        write_value: 0

poll_rate: 1s                         # Global default poll rate

ui:
  theme: default                       # Color theme (F6 to cycle)
```

## PLC Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier for the PLC |
| `address` | string | Yes | IP address or hostname |
| `family` | string | Yes | `logix`, `micro800`, `slc500`, `plc5`, `micrologix`, `s7`, `beckhoff`, `omron` |
| `slot` | int | No | CPU slot number (default: 0) |
| `connection_path` | string | No | CIP route path for Logix (see [PLC Setup](plc-setup.md#connection-path-cip-routing)) |
| `enabled` | bool | No | Auto-connect on startup (default: false) |
| `health_check_enabled` | bool | No | Publish health status (default: true) |
| `poll_rate` | duration | No | Per-PLC poll rate (overrides global) |
| `tags` | list | No | Tags to publish |

### Logix-specific Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `connection_path` | string | No | CIP route as comma-separated port,address pairs (e.g., `1,4,2,192.168.1.1,1,0`) |

When empty (the default), WarLink routes to backplane port 1 at the configured slot (`1,{slot}`). Only set this when routing through communication modules or across chassis. See [Connection Path](plc-setup.md#connection-path-cip-routing) for details and examples.

### Beckhoff-specific Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ams_net_id` | string | Yes | PLC's AMS Net ID (e.g., `192.168.1.100.1.1`) |
| `ams_port` | int | No | AMS port (default: 851 for TwinCAT 3) |

### Omron-specific Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `protocol` | string | No | `fins` (default), `fins-tcp`, `fins-udp`, or `eip` |
| `fins_port` | int | No | FINS port (default: 9600) - FINS only |
| `fins_network` | int | No | FINS network number (default: 0) - FINS only |
| `fins_node` | int | No | FINS node number (default: 0) - FINS only |
| `fins_unit` | int | No | CPU unit number (default: 0) - FINS only |

**Protocol Options:**

| Value | Description | Use Case |
|-------|-------------|----------|
| `fins` | FINS with auto TCP/UDP (tries TCP first) | Default for CS/CJ/CP series |
| `fins-tcp` | Force FINS over TCP | When you know PLC supports TCP |
| `fins-udp` | Force FINS over UDP | Older PLCs or network restrictions |
| `eip` | EtherNet/IP (CIP) | NJ/NX series (symbolic tags) |

**Notes:**
- Use `protocol: eip` for NJ/NX series PLCs for best performance
- EIP uses symbolic tag names and supports automatic tag discovery (tags and types are discovered, but UDT/structure members are not unpacked)
- FINS fields (`fins_port`, `fins_node`, etc.) are ignored when using EIP
- EIP uses TCP port 44818 (standard EtherNet/IP port)

### PCCC-specific Fields (SLC-500, PLC/5, MicroLogix) — Experimental

> **Experimental:** PCCC support is untested against real hardware.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `connection_path` | string | No | CIP route for gateway routing (e.g., through a ControlLogix backplane) |

Tags use data table addresses (e.g., `N7:0`, `F8:5`, `T4:0.ACC`) and must always include a `data_type`. Supported types: INT, FLOAT, BINARY, TIMER, COUNTER, CONTROL, STRING, LONG.

**Example:**
```yaml
- name: SLC500
  address: 192.168.1.50
  family: slc500
  enabled: true
  tags:
    - name: "N7:0"
      alias: Counter
      data_type: INT
      enabled: true
    - name: "F8:0"
      alias: Temperature
      data_type: FLOAT
      enabled: true
```

See [PLC Setup - SLC-500 / PLC-5 / MicroLogix](plc-setup.md#allen-bradley-slc-500--plc-5--micrologix-experimental) for details.

## Tag Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Tag name or address |
| `alias` | string | No | Friendly name for publishing (S7/Omron) |
| `data_type` | string | No | Data type for manual tags (S7/Omron FINS) |
| `enabled` | bool | No | Enable publishing (default: false) |
| `writable` | bool | No | Allow write operations (default: false) |
| `ignore_changes` | list | No | UDT member names to ignore for change detection |
| `no_rest` | bool | No | Exclude from REST API |
| `no_mqtt` | bool | No | Exclude from MQTT publishing |
| `no_kafka` | bool | No | Exclude from Kafka publishing |
| `no_valkey` | bool | No | Exclude from Valkey publishing |

### Tag Examples

**Allen-Bradley (automatic discovery):**
```yaml
tags:
  - name: Program:MainProgram.Counter
    enabled: true
    writable: true
  - name: Program:MainProgram.MachineStatus
    enabled: true
    ignore_changes: [Timestamp, HeartbeatCount]  # Don't republish when these UDT members change
```

**Siemens S7 (manual addressing):**
```yaml
tags:
  - name: DB1.0
    alias: ProductCount           # Published as "ProductCount" instead of "DB1.0"
    data_type: DINT
    enabled: true
  - name: DB1.4
    alias: Temperature
    data_type: REAL
    enabled: true
    writable: true
  - name: DB1.8.0                 # Bit 0 of byte 8
    alias: MachineRunning
    data_type: BOOL
    enabled: true
  - name: DB1.10[100]             # Array of 100 bytes starting at offset 10
    alias: ProductName
    data_type: STRING
    enabled: true
```

**Omron FINS (manual addressing):**
```yaml
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
  - name: CIO50.5                 # Bit 5 of CIO50
    alias: ConveyorRunning
    data_type: BOOL
    enabled: true
```

### Per-Service Publishing

Use `no_*` flags to exclude specific tags from services:

```yaml
tags:
  - name: HighFrequencyCounter
    enabled: true
    no_mqtt: true                 # Don't publish to MQTT (too frequent)
    no_valkey: true               # Don't store in Redis
                                  # Still published to REST and Kafka
```

### Change Detection Filtering

For UDTs with volatile members (timestamps, heartbeats), use `ignore_changes` to prevent republishing when only those members change:

```yaml
tags:
  - name: MachineStatus           # UDT tag
    enabled: true
    ignore_changes:
      - Timestamp                 # These members are still included in published data
      - HeartbeatCount            # but changes to them alone don't trigger republishing
      - SequenceNumber
```

## Web Server Configuration

The `web:` key configures the built-in web server that hosts both the REST API and the browser-based management UI.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | bool | No | Enable the web server (default: false) |
| `host` | string | No | Bind address (default: 0.0.0.0) |
| `port` | int | No | HTTP port (default: 8080) |
| `api.enabled` | bool | No | Enable the REST API (default: true) |
| `ui.enabled` | bool | No | Enable the browser UI (default: false) |
| `ui.session_secret` | string | No | Base64-encoded session secret (auto-generated) |
| `ui.users` | list | No | Web UI user accounts |

### Web UI User Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `username` | string | Yes | Login username |
| `password_hash` | string | Yes | Bcrypt-hashed password |
| `role` | string | Yes | `admin` or `viewer` |

Users are typically managed through the web UI itself or via the `--admin-user` and `--admin-pass` command-line flags. You do not need to generate bcrypt hashes manually.

See the [Web UI Guide](web-ui.md) for details on using the browser interface.


## MQTT Configuration

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier |
| `enabled` | bool | No | Enable this broker (default: false) |
| `broker` | string | Yes | Broker hostname or IP |
| `port` | int | No | Broker port (default: 1883) |
| `client_id` | string | Yes | MQTT client ID |
| `selector` | string | No | Optional sub-namespace within the global namespace |
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
| `selector` | string | No | Optional sub-namespace within the global namespace |
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
| `selector` | string | No | Optional sub-namespace |
| `publish_changes` | bool | No | Publish tag changes |
| `use_tls` | bool | No | Enable TLS (default: false) |
| `tls_skip_verify` | bool | No | Skip TLS certificate verification |
| `sasl_mechanism` | string | No | SASL auth: PLAIN, SCRAM-SHA-256, SCRAM-SHA-512 |
| `username` | string | No | SASL username |
| `password` | string | No | SASL password |
| `required_acks` | int | No | Acks required: -1=all, 0=none, 1=leader |
| `max_retries` | int | No | Max publish retries |
| `retry_backoff` | duration | No | Retry backoff interval |
| `auto_create_topics` | bool | No | Auto-create topics if missing (default: true) |
| `enable_writeback` | bool | No | Enable consuming write requests |
| `consumer_group` | string | No | Consumer group ID (default: warlink-{name}-writers) |
| `write_max_age` | duration | No | Max age of write requests to process (default: 2s) |

## TagPack Configuration

```yaml
tag_packs:
  - name: ProductionMetrics
    enabled: true
    mqtt_enabled: true
    kafka_enabled: true
    valkey_enabled: true
    members:
      - plc: MainPLC
        tag: ProductCount
      - plc: MainPLC
        tag: Temperature
      - plc: SecondaryPLC
        tag: ConveyorSpeed
        ignore_changes: true      # Changes don't trigger republish
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique pack identifier |
| `enabled` | bool | No | Enable the pack (default: false) |
| `mqtt_enabled` | bool | No | Publish to MQTT brokers |
| `kafka_enabled` | bool | No | Publish to Kafka clusters |
| `valkey_enabled` | bool | No | Store/publish to Valkey/Redis |
| `members` | list | Yes | Tags to include in the pack |

### TagPack Member Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `plc` | string | Yes | PLC name |
| `tag` | string | Yes | Tag name (uses alias if set) |
| `ignore_changes` | bool | No | If true, changes don't trigger pack publish |

## Rule Configuration

Rules monitor PLC tag conditions and execute actions when conditions are met. Each rule supports multiple conditions (AND/OR logic), multiple action types, and optional cleared actions on falling edge.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Unique identifier |
| `enabled` | bool | No | Enable this rule (default: false) |
| `conditions` | list | Yes | One or more conditions |
| `logic_mode` | string | No | `and` (default) or `or` |
| `debounce_ms` | int | No | Debounce time in milliseconds |
| `cooldown_ms` | int | No | Minimum interval before re-arming (milliseconds) |
| `actions` | list | Yes | Actions to execute on rising edge |
| `cleared_actions` | list | No | Actions to execute on falling edge |

### Condition Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `plc` | string | Yes | PLC name |
| `tag` | string | Yes | Tag to monitor |
| `operator` | string | Yes | Comparison: `==`, `!=`, `>`, `<`, `>=`, `<=` |
| `value` | any | Yes | Value to compare against |
| `not` | bool | No | Invert the condition result |

### Action Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | string | Yes | Action type: `publish`, `webhook`, or `writeback` |
| `name` | string | No | Label for logging/UI |

**Publish action fields (`type: publish`):**

| Field | Type | Description |
|-------|------|-------------|
| `tag_or_pack` | string | Tag name or `pack:Name` for TagPacks |
| `include_trigger` | bool | Include condition tag values in message |
| `mqtt_broker` | string | MQTT broker: `all`, `none`, or specific name |
| `mqtt_topic` | string | Custom MQTT sub-topic (default: `rules/{rule-name}`) |
| `kafka_cluster` | string | Kafka cluster: `all`, `none`, or specific name |
| `kafka_topic` | string | Custom Kafka topic suffix (default: `rules`) |

**Webhook action fields (`type: webhook`):**

| Field | Type | Description |
|-------|------|-------------|
| `url` | string | Target HTTP endpoint |
| `method` | string | HTTP method: GET, POST, PUT, PATCH (default: POST) |
| `content_type` | string | Request Content-Type (default: application/json) |
| `headers` | map | Custom HTTP headers |
| `body` | string | Request body template with `#PLCName.tagName` references |
| `auth` | object | Authentication configuration |
| `timeout` | duration | HTTP request timeout (default: 30s) |

**Writeback action fields (`type: writeback`):**

| Field | Type | Description |
|-------|------|-------------|
| `write_plc` | string | PLC name (defaults to first condition's PLC) |
| `write_tag` | string | Tag to write to |
| `write_value` | any | Value to write |

### Authentication

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Auth type: `bearer`, `basic`, `jwt`, `custom_header` |
| `token` | string | Token for bearer/jwt auth |
| `username` | string | Username for basic auth |
| `password` | string | Password for basic auth |
| `header_name` | string | Header name for custom_header auth |
| `header_value` | string | Header value for custom_header auth |

### Rule Example

```yaml
rules:
  - name: ProductComplete
    enabled: true
    conditions:
      - plc: MainPLC
        tag: Program:MainProgram.ProductReady
        operator: "=="
        value: true
    debounce_ms: 100
    cooldown_ms: 1000
    actions:
      - type: publish
        tag_or_pack: pack:ProductionMetrics
        include_trigger: true
        mqtt_broker: all
        kafka_cluster: ProductionKafka
        kafka_topic: production-events
      - type: writeback
        write_tag: Program:MainProgram.ProductAck
        write_value: 1
    cleared_actions:
      - type: writeback
        write_tag: Program:MainProgram.ProductAck
        write_value: 0
```

See [Rules Engine](rules.md) for the full rules documentation.

## UI Configuration

```yaml
ui:
  theme: default    # Theme name
```

| Field | Type | Description |
|-------|------|-------------|
| `theme` | string | Color theme name |

### Available Themes

| Theme | Description |
|-------|-------------|
| `default` | Clean white/silver/gray with green/red status (high ANSI compatibility) |
| `retro` | Classic green phosphor CRT terminal |
| `mono` | Blue IBM terminal aesthetic |
| `amber` | Warm amber CRT with orange accents |
| `highcontrast` | High contrast for accessibility |
| `vanderbilt` | Vanderbilt University gold and black |
| `harvard` | Harvard University crimson and gray |
| `lsu` | LSU purple and gold |
| `redwings` | Detroit Red Wings red and white |
| `lions` | Detroit Lions blue and silver |
| `spartans` | Michigan State green and white |
| `tigers` | Detroit Tigers navy and orange |

Press `F6` to cycle through themes. The selection is saved automatically.

## Duration Format

Duration values accept Go-style duration strings:
- `500ms` - 500 milliseconds
- `1s` - 1 second
- `1m30s` - 1 minute 30 seconds
- `1h` - 1 hour
