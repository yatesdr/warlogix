# WarLogix

A TUI (Text User Interface) gateway application for industrial PLCs including Allen-Bradley ControlLogix/CompactLogix, Siemens S7, and Beckhoff TwinCAT. Browse tags, monitor values in real-time, and republish data via REST API, MQTT brokers, Kafka, and Redis/Valkey.


## The name

WAR stands for "whispers across realms" - this application is intended to provide a gateway between industrial and IT applications - connecting PLCs from multiple vendors to REST / MQTT / Kafka / Valkey (Redis) formats.

## Features

- **PLC Discovery**: Automatically discover PLCs on your network via UDP broadcast or direct TCP connection
- **Tag Browser**: Browse controller and program-scoped tags with real-time value updates
- **REST API**: Expose PLC tag values via a REST API for integration with other systems
- **MQTT Publishing**: Publish tag values to MQTT brokers on change, with retained messages
- **MQTT Write-back**: Write to PLC tags via MQTT with proper type conversion and validation
- **MQTT Authentication**: Username/password authentication for secured brokers
- **MQTT TLS/SSL**: Encrypted connections to MQTT brokers
- **Valkey/Redis Publishing**: Store tag values in Valkey/Redis with key-value storage and Pub/Sub notifications
- **Valkey Write-back**: Write to PLC tags via Valkey LIST queue with response notifications
- **Valkey TLS/SSL**: Encrypted connections to Valkey/Redis servers
- **Kafka Publishing**: Publish tag changes to Apache Kafka topics with exactly-once semantics
- **Event Triggers**: Capture data snapshots on PLC events and publish to Kafka with acknowledgment support
- **Multi-PLC Support**: Connect to and manage multiple PLCs simultaneously (12+ PLCs supported)
- **Array Support**: Arrays of known types are published as native JSON arrays
- **Configuration Persistence**: Save your PLC connections, tag selections, and settings to YAML

## Limitations

- Structs and UDTs are published as raw byte arrays (not decoded into fields)
- Only limited testing has been done on ControlLogix PLCs
- This is a BETA release and will be improved as bugs are identified and remediated

## Warnings

- This software allows reading and writing to industrial PLC's, which can present hazards if done poorly.
- No warranty or liability is assumed - use at your own risk.

## Supported PLCs

### Allen-Bradley (Rockwell Automation)
- ControlLogix (1756 series) - Full support with automatic tag discovery
- CompactLogix (1769 series) - Full support with automatic tag discovery
- Micro800 series (Micro820, Micro830, Micro850) - Full support with automatic tag discovery, tested on Micro820

### Siemens
- S7-300/400 - Manual tag configuration with S7 addressing (DB, I, Q, M areas)
- S7-1200/1500 - Manual tag configuration with S7 addressing

### Beckhoff
- TwinCAT 2/3 PLCs - Full support with automatic symbol discovery via ADS protocol
- Requires AMS Net ID and port configuration (default port 851 for TwinCAT 3)

## Basic Usage

Download the binary, or build from source, and run the application.

### Adding PLCs
- Navigate between tabs by pressing Shift-Tab.
- Use the hot-keys to add one or more PLC's, edit settings, browse tags, and configure REST and MQTT re-publishing.
- By default, no tags will be synced.   Use the Tag Browser tab to select tags to publish and set the status.
- If you are on the same switch as the PLC you may be able to discover available PLC's by pressing 'd', but in many situations the UDP discovery is limited by broadcast domain and you will need to add the PLC by the IP Address.

<img width="822" height="537" alt="image" src="https://github.com/user-attachments/assets/e57a3ff9-bd15-4943-911a-ebb8567aadcc" />

### Browsing Tags

- To add tags for read / republish you can select them in the tag browser (Shift-Tab to tab to it)
<img width="816" height="539" alt="image" src="https://github.com/user-attachments/assets/a87fc123-bd94-4f19-a5e7-2c748b34449e" />


### Local REST Endpoint

- The REST endpoint is read-only, but useful for polling state.
<img width="821" height="540" alt="image" src="https://github.com/user-attachments/assets/899f2be1-7adb-453c-a35e-df87aeb07d9b" />


### MQTT Re-Publishing

- The MQTT re-publisher can republish to any accessible MQTT broker with optional username/password authentication and TLS encryption. This is the primary way to move data into the IT world as it can punch out of the typical machine network onto the IT side with a proper firewall config.
- MQTT protocol offers write-back for write-enabled tags with a properly formatted write request (see more below).  This is only tested on basic types and should not be used as part of a control system.   It is intended for ack / clear requests to the PLC.

<img width="817" height="540" alt="image" src="https://github.com/user-attachments/assets/c5e459c9-65f0-4fa9-92c6-c3b9fd8cb401" />

### Redis / Valkey Re-Publishing

- Great for real-time dashboards or status, republish to any Valkey or Redis server accessible from the application.

<img width="822" height="542" alt="image" src="https://github.com/user-attachments/assets/474057b9-de37-4694-acf3-bf4936352563" />


## Installation

### From Source

```bash
git clone https://github.com/yatesdr/warlogix.git
cd warlogix
go build -o warlogix ./cmd/warlogix
```

### Cross-Platform Builds

Build for all platforms:

```bash
make all
```

Or build for specific platforms:

```bash
make linux
make windows
make macos
```

## Usage

```bash
# Run with default config (~/.warlogix/config.yaml)
./warlogix

# Run with custom config file
./warlogix -config /path/to/config.yaml
```

## Keyboard Shortcuts

### Navigation
- `Shift+Tab` - Switch between tabs
- `Tab` - Move between fields
- `Enter` - Select / Activate
- `Escape` - Close dialog / Back
- `?` - Show help
- `Q` - Quit

### PLCs Tab
- `d` - Discover PLCs
- `a` - Add PLC
- `e` - Edit selected
- `r` - Remove selected
- `c` - Connect
- `C` - Disconnect

### Tag Browser Tab
- `/` - Focus filter
- `c` - Clear filter
- `p` - Focus PLC dropdown
- `Space` - Toggle tag publishing
- `w` - Toggle tag writable
- `d` - Show tag details

### MQTT Tab
- `a` - Add broker
- `e` - Edit selected
- `r` - Remove selected
- `c` - Connect
- `C` - Disconnect

### Valkey Tab
- `a` - Add server
- `e` - Edit selected
- `r` - Remove selected
- `c` - Connect
- `C` - Disconnect

### Kafka Tab
- `a` - Add cluster
- `e` - Edit selected
- `r` - Remove selected
- `c` - Connect
- `C` - Disconnect

### Triggers Tab
- `a` - Add trigger
- `e` - Edit selected trigger
- `r` - Remove selected trigger
- `t` - Add data tag to selected trigger
- `x` - Remove data tag from selected trigger
- `s` - Start/arm trigger
- `S` - Stop trigger
- `T` - Test fire trigger (manual test)
- `Tab` - Switch between triggers table and data tags

## Configuration

Configuration is stored in YAML format at `~/.warlogix/config.yaml` - you do not generally need to manually edit this:

```yaml
plcs:
  # Allen-Bradley Logix PLC (automatic tag discovery)
  - name: MainPLC
    address: 192.168.1.100
    family: logix          # or omit for default
    slot: 0
    enabled: true
    tags:
      - name: Program:MainProgram.Counter
        enabled: true
        writable: true

  # Siemens S7 PLC (manual tag configuration)
  - name: SiemensPLC
    address: 192.168.1.101
    family: s7
    slot: 1                # S7 slot (typically 1 for S7-300/400)
    enabled: true
    tags:
      - name: DB1.DBD0
        data_type: DINT
        enabled: true
      - name: DB1.DBX4.0
        data_type: BOOL
        enabled: true

  # Beckhoff TwinCAT PLC (automatic symbol discovery)
  - name: TwinCAT
    address: 192.168.1.102
    family: beckhoff
    ams_net_id: 192.168.1.102.1.1  # AMS Net ID (IP.1.1 is common)
    ams_port: 851                   # TwinCAT 3 PLC runtime 1
    enabled: true
    tags:
      - name: MAIN.Temperature
        enabled: true
      - name: GVL.ProductCounter
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
    username: ""           # optional
    password: ""           # optional
    use_tls: false         # set true for SSL/TLS
    client_id: warlogix-main
    root_topic: factory

valkey:
  - name: LocalValkey
    enabled: true
    address: localhost:6379
    password: ""           # optional
    database: 0            # Redis DB number
    factory: factory       # Key prefix (like root_topic for MQTT)
    use_tls: false         # set true for SSL/TLS
    key_ttl: 0s            # TTL for keys (0 = no expiry)
    publish_changes: true  # Publish to Pub/Sub on changes
    enable_writeback: false # Enable write-back queue

kafka:
  - name: LocalKafka
    enabled: true
    brokers:
      - localhost:9092
    topic: plc-events      # Default topic for triggers
    publish_changes: false # Set true to publish all tag changes (separate from triggers)

triggers:
  - name: ProductComplete
    enabled: true
    plc: MainPLC
    trigger_tag: Program:MainProgram.ProductReady  # Tag to monitor
    condition:
      operator: "=="       # ==, !=, >, <, >=, <=
      value: true          # Fire when tag equals this value
    ack_tag: Program:MainProgram.ProductAck        # Optional: write 1=success, -1=error
    debounce_ms: 100       # Minimum time between fires
    tags:                  # Data tags to capture when trigger fires
      - Program:MainProgram.ProductID
      - Program:MainProgram.BatchNumber
      - Program:MainProgram.Quantity
    kafka_cluster: LocalKafka
    topic: production-events
    metadata:              # Optional static metadata
      line: "Line1"
      station: "Station5"

poll_rate: 1s
```

## REST API

When enabled, the REST API provides the following endpoints:

- `GET /` - List all PLCs with status
- `GET /{plc_name}` - PLC details and identity
- `GET /{plc_name}/tags` - All tags with current values
- `GET /{plc_name}/tags/{tagname}` - Single tag value

## MQTT Topics

### Publishing (Tag Values)
Tags are published to: `{root_topic}/{plc_name}/tags/{tag_name}`

Message format:
```json
{
  "topic": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 42,
  "type": "DINT",
  "writable": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Write Requests
Send write requests to: `{root_topic}/{plc_name}/write`

Request format:
```json
{
  "topic": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100
}
```

Response on: `{root_topic}/{plc_name}/write/response`
```json
{
  "topic": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100,
  "success": true,
  "timestamp": "2024-01-15T10:30:05Z"
}
```

## Valkey/Redis Integration

### Key Structure
Tags are stored with the key pattern: `{factory}:{plc}:tags:{tag}`

Value format (JSON):
```json
{
  "factory": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 42,
  "type": "DINT",
  "writable": true,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Pub/Sub Channels
When `publish_changes` is enabled, changes are published to:
- `{factory}:{plc}:changes` - Per-PLC change notifications
- `{factory}:_all:changes` - All changes across all PLCs

### Write-back Queue
When `enable_writeback` is enabled, write requests can be sent via a LIST queue:
- Queue: `{factory}:writes` (use RPUSH to add requests)
- Responses: `{factory}:write:responses` (Pub/Sub channel)

Write request format:
```json
{
  "factory": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100
}
```

Write response format:
```json
{
  "factory": "factory",
  "plc": "MainPLC",
  "tag": "Counter",
  "value": 100,
  "success": true,
  "timestamp": "2024-01-15T10:30:05Z"
}
```

### TTL for Stale Detection
Set `key_ttl` to a duration (e.g., `30s`, `1m`) to automatically expire keys. This allows consumers to detect stale data when the gateway stops updating.

## Event Triggers

Event Triggers provide a way to capture data from a PLC when a specific condition is met, and publish the captured data to Kafka. This is useful for event-driven architectures where you need to capture a snapshot of process data at a specific moment (e.g., when a product is complete, when a fault occurs, or when a cycle finishes).

### How Triggers Work

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           TRIGGER DATA FLOW                             │
└─────────────────────────────────────────────────────────────────────────┘

  ┌─────────┐         ┌──────────────┐         ┌─────────────────┐
  │   PLC   │────────▶│   WarLogix   │────────▶│     Kafka       │
  │         │         │   Trigger    │         │     Topic       │
  └─────────┘         └──────────────┘         └─────────────────┘
       │                     │                         │
       │                     │                         ▼
       │  1. Monitor         │  3. Publish        ┌─────────┐
       │     Trigger Tag     │     JSON           │ Consumer│
       │                     │     Message        │  App    │
       ▼                     ▼                    └─────────┘
  ┌─────────┐         ┌──────────────┐
  │ Trigger │         │  2. Capture  │
  │   Tag   │────────▶│     Data     │
  │ = true  │ (edge)  │     Tags     │
  └─────────┘         └──────────────┘
       │                     │
       │                     │  4. Write Ack
       │                     │     (1=ok, -1=err)
       ▼                     ▼
  ┌─────────┐         ┌──────────────┐
  │   Ack   │◀────────│   Ack Tag    │
  │   Tag   │         │   Written    │
  └─────────┘         └──────────────┘
```

1. **Monitor**: The trigger continuously monitors a "trigger tag" on the PLC (polling every 100ms)
2. **Edge Detection**: When the trigger tag transitions from NOT meeting the condition to meeting it (rising edge), the trigger fires
3. **Capture**: All configured "data tags" are read from the PLC and packaged into a JSON message
4. **Publish**: The message is sent to the configured Kafka topic
5. **Acknowledge**: If an ack tag is configured, WarLogix writes `1` for success or `-1` for error

### Trigger States

| State | Description |
|-------|-------------|
| **Disabled** | Trigger is not running |
| **Armed** | Trigger is running and waiting for condition |
| **Firing** | Trigger condition met, capturing and sending data |
| **Cooldown** | Waiting for trigger tag to reset before re-arming |
| **Error** | An error occurred (will auto-recover on condition reset) |

### Condition Operators

| Operator | Description | Example |
|----------|-------------|---------|
| `==` | Equal | `value == true` |
| `!=` | Not equal | `value != 0` |
| `>` | Greater than | `value > 100` |
| `<` | Less than | `value < 50` |
| `>=` | Greater or equal | `value >= 10` |
| `<=` | Less or equal | `value <= 1000` |

### Kafka Message Format

When a trigger fires, it publishes a JSON message to Kafka:

```json
{
  "trigger": "ProductComplete",
  "timestamp": "2024-01-15T10:30:00.123456789Z",
  "sequence": 42,
  "plc": "MainPLC",
  "metadata": {
    "line": "Line1",
    "station": "Station5"
  },
  "data": {
    "Program:MainProgram.ProductID": 12345,
    "Program:MainProgram.BatchNumber": "BATCH-001",
    "Program:MainProgram.Quantity": 100
  }
}
```

| Field | Description |
|-------|-------------|
| `trigger` | Name of the trigger that fired |
| `timestamp` | UTC timestamp with nanosecond precision |
| `sequence` | Monotonically increasing sequence number (unique per session) |
| `plc` | Name of the PLC the data was captured from |
| `metadata` | Static key-value pairs configured on the trigger |
| `data` | Captured tag values at the moment the trigger fired |

### Acknowledgment Tags

The ack tag feature allows the PLC program to know when data has been successfully captured:

- **Success (1)**: Data was captured and sent to Kafka successfully
- **Error (-1)**: An error occurred during capture or send

This enables the PLC program to:
- Wait for acknowledgment before proceeding
- Retry or alarm if an error occurs
- Track that events were successfully recorded

### Example Use Cases

**Production Tracking**: Capture product data when a "part complete" signal goes true
```yaml
triggers:
  - name: PartComplete
    plc: LineController
    trigger_tag: Part_Complete
    condition: { operator: "==", value: true }
    tags: [Part_ID, Serial_Number, Cycle_Time, Quality_Score]
    kafka_cluster: Production
    topic: part-completions
```

**Fault Recording**: Capture machine state when a fault occurs
```yaml
triggers:
  - name: MachineFault
    plc: Machine1
    trigger_tag: Fault_Active
    condition: { operator: "==", value: true }
    tags: [Fault_Code, Axis_Position, Motor_Current, Last_Command]
    kafka_cluster: Maintenance
    topic: machine-faults
```

**Batch Completion**: Capture batch summary when batch count reaches target
```yaml
triggers:
  - name: BatchDone
    plc: BatchController
    trigger_tag: Batch_Count
    condition: { operator: ">=", value: 1000 }
    ack_tag: Batch_Ack
    tags: [Batch_ID, Total_Count, Good_Count, Reject_Count, Start_Time, End_Time]
    kafka_cluster: Quality
    topic: batch-reports
```

## Supported Data Types

The following PLC data types are fully supported for reading and writing:

| Type   | Description           | Range                          |
|--------|-----------------------|--------------------------------|
| BOOL   | Boolean               | true/false                     |
| SINT   | 8-bit signed integer  | -128 to 127                    |
| INT    | 16-bit signed integer | -32,768 to 32,767              |
| DINT   | 32-bit signed integer | -2,147,483,648 to 2,147,483,647|
| LINT   | 64-bit signed integer | Full int64 range               |
| USINT  | 8-bit unsigned        | 0 to 255                       |
| UINT   | 16-bit unsigned       | 0 to 65,535                    |
| UDINT  | 32-bit unsigned       | 0 to 4,294,967,295             |
| ULINT  | 64-bit unsigned       | Full uint64 range              |
| REAL   | 32-bit float          | IEEE 754 single precision      |
| LREAL  | 64-bit float          | IEEE 754 double precision      |
| STRING | String                | Up to 82 characters            |

### Arrays

Arrays of the above types are published as native JSON arrays:

```json
{
  "tag": "MyDintArray",
  "type": "DINT[]",
  "value": [100, 200, 300, 400, 500]
}
```

### Structs and UDTs

Structs and UDTs are published as byte arrays for manual decoding:

```json
{
  "tag": "MyUDT",
  "type": "STRUCT",
  "value": [212, 153, 4, 0, 0, 0, 128, 0, ...]
}
```

Each element represents one byte (0-255). Decode using the UDT's field layout (little-endian byte order).

## Beckhoff TwinCAT Configuration

### AMS Net ID

The AMS Net ID is a unique identifier for each TwinCAT device on the network. It typically follows the pattern `IP.1.1` where IP is the device's IP address split into four parts.

Example: For IP `192.168.1.100`, the AMS Net ID would be `192.168.1.100.1.1`

If not specified, WarLogix will attempt to derive the AMS Net ID from the IP address.

### AMS Ports

Common TwinCAT AMS ports:

| Port | Description |
|------|-------------|
| 851  | TwinCAT 3 PLC Runtime 1 (default) |
| 852  | TwinCAT 3 PLC Runtime 2 |
| 801  | TwinCAT 2 PLC Runtime 1 |
| 811  | TwinCAT 2 PLC Runtime 2 |

### Symbol Names

Beckhoff symbols use dot notation to reference variables:

- `MAIN.Variable` - Variable in MAIN program
- `GVL.GlobalVar` - Variable in Global Variable List
- `FB_Instance.Member` - Member of a function block instance

### Automatic Discovery

When connecting to a Beckhoff PLC, WarLogix automatically discovers all available symbols from the TwinCAT symbol table. This includes:

- Global variables (GVL)
- Program variables (MAIN, etc.)
- Function block instances and their members

Primitive types (BOOL, INT, DINT, REAL, etc.) are automatically recognized. Complex types like structures show their type name but may require manual interpretation.

## Roadmap

 - General stability (priority 1)
 - Improve overall application speed
 - Add more debugging and logging

## License

Apache License 2.0 - see LICENSE file for details.

## Acknowledgements

 - Pylogix / dmroeder - reference code was used to help identify and troubleshoot throughout the development process.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
