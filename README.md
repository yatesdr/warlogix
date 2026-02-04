# WarLogix

A TUI (Text User Interface) gateway application for industrial PLCs including Allen-Bradley ControlLogix/CompactLogix, Siemens S7, Beckhoff TwinCAT, and Omron FINS. Browse tags, monitor values in real-time, and republish data via REST API, MQTT brokers, Kafka, and Redis/Valkey.

## The name

WAR stands for "whispers across realms" - this application provides a gateway between industrial and IT applications, connecting PLCs from multiple vendors to REST / MQTT / Kafka / Valkey (Redis) formats.

## Features

- **Multi-PLC Support**: Connect to Allen-Bradley Logix, Siemens S7, Beckhoff TwinCAT, and Omron PLCs simultaneously
- **Tag Browser**: Browse controller tags with real-time value updates (automatic discovery for Logix/Beckhoff)
- **REST API**: Expose PLC tag values via HTTP for integration with other systems
- **MQTT Publishing**: Publish tag values to MQTT brokers with optional write-back, authentication, and TLS
- **Valkey/Redis**: Store tag values with Pub/Sub notifications and write-back queue support
- **Kafka**: Publish tag changes and event triggers to Apache Kafka topics
- **Event Triggers**: Capture data snapshots on PLC events and publish to Kafka with acknowledgment
- **Array Support**: Arrays of known types are published as native JSON arrays
- **Auto-Reconnection**: Automatic connection recovery with watchdog monitoring

## Warnings

- This software allows reading and writing to industrial PLCs, which can present hazards if done poorly.
- No warranty or liability is assumed - use at your own risk.

## Supported PLCs

| Family | Models | Tag Discovery | Notes |
|--------|--------|---------------|-------|
| **Allen-Bradley** | ControlLogix (1756), CompactLogix (1769), Micro800 | Automatic | Full EtherNet/IP support |
| **Siemens** | S7-300/400/1200/1500 | Manual | Requires PUT/GET enabled |
| **Beckhoff** | TwinCAT 2/3 | Automatic | Requires AMS Net ID configuration |
| **Omron** | CJ/CS/CP/NJ/NX Series | Manual | FINS/UDP protocol |

## Installation

```bash
git clone https://github.com/yatesdr/warlogix.git
cd warlogix
go build -o warlogix ./cmd/warlogix
```

Or use `make all` to build for all platforms.

## Usage

```bash
./warlogix                          # Default config (~/.warlogix/config.yaml)
./warlogix -config /path/to/config  # Custom config file
```

## Basic Usage

- Navigate between tabs with `Shift+Tab`
- Press `a` to add PLCs, `d` to discover (same broadcast domain only)
- Use the Tag Browser to select tags for publishing
- Configure MQTT/Valkey/Kafka connections for republishing

<img width="822" height="537" alt="image" src="https://github.com/user-attachments/assets/e57a3ff9-bd15-4943-911a-ebb8567aadcc" />

## Keyboard Shortcuts

| Tab | Key | Action |
|-----|-----|--------|
| Global | `Shift+Tab` | Switch tabs |
| Global | `?` | Help |
| Global | `Q` | Quit |
| PLCs | `d/a/e/r` | Discover/Add/Edit/Remove |
| PLCs | `c/C` | Connect/Disconnect |
| Browser | `/` | Focus filter |
| Browser | `Space/w` | Toggle publish/writable |
| MQTT/Valkey/Kafka | `a/e/r/c/C` | Add/Edit/Remove/Connect/Disconnect |
| Triggers | `a/e/r` | Add/Edit/Remove trigger |
| Triggers | `t/x` | Add/Remove data tag |
| Triggers | `s/S/T` | Start/Stop/Test trigger |

## Configuration

Configuration is stored in YAML at `~/.warlogix/config.yaml`:

```yaml
plcs:
  # Allen-Bradley (automatic tag discovery)
  - name: MainPLC
    address: 192.168.1.100
    family: logix
    slot: 0
    enabled: true
    tags:
      - name: Program:MainProgram.Counter
        enabled: true
        writable: true

  # Siemens S7 (manual tags with byte offsets)
  - name: SiemensPLC
    address: 192.168.1.101
    family: s7
    slot: 1
    tags:
      - name: DB1.DBD0
        data_type: DINT
        enabled: true

  # Beckhoff TwinCAT (automatic symbol discovery)
  - name: TwinCAT
    address: 192.168.1.102
    family: beckhoff
    ams_net_id: 192.168.1.102.1.1
    ams_port: 851
    tags:
      - name: MAIN.Temperature
        enabled: true

  # Omron FINS (manual tags with memory areas)
  - name: OmronPLC
    address: 192.168.1.103
    family: omron
    fins_port: 9600
    fins_node: 0
    tags:
      - name: DM100
        data_type: DINT
        enabled: true

rest:
  enabled: true
  port: 8080

mqtt:
  - name: LocalBroker
    broker: localhost
    port: 1883
    root_topic: factory
    # Optional: username, password, use_tls

valkey:
  - name: LocalValkey
    address: localhost:6379
    factory: factory
    # Optional: password, use_tls, key_ttl, enable_writeback

kafka:
  - name: LocalKafka
    brokers: [localhost:9092]
    topic: plc-events

triggers:
  - name: ProductComplete
    plc: MainPLC
    trigger_tag: Program:MainProgram.ProductReady
    condition: { operator: "==", value: true }
    ack_tag: Program:MainProgram.ProductAck
    tags: [ProductID, BatchNumber, Quantity]
    kafka_cluster: LocalKafka
    topic: production-events

poll_rate: 1s
```

## REST API

| Endpoint | Description |
|----------|-------------|
| `GET /` | List all PLCs with status |
| `GET /{plc}/tags` | All tags with current values |
| `GET /{plc}/tags/{tag}` | Single tag value |

## MQTT Topics

- **Publish**: `{root_topic}/{plc}/tags/{tag}` - Tag values with metadata
- **Write**: `{root_topic}/{plc}/write` - Send write requests
- **Response**: `{root_topic}/{plc}/write/response` - Write confirmations

## Valkey/Redis

- **Keys**: `{factory}:{plc}:tags:{tag}` - JSON tag values
- **Pub/Sub**: `{factory}:{plc}:changes` - Change notifications
- **Write Queue**: `{factory}:writes` (LIST) - Write requests via RPUSH

## Supported Data Types

| Type | Size | Description |
|------|------|-------------|
| BOOL | 1 bit | Boolean |
| SINT/BYTE | 1 byte | 8-bit signed/unsigned |
| INT/WORD | 2 bytes | 16-bit signed/unsigned |
| DINT/DWORD | 4 bytes | 32-bit signed/unsigned |
| LINT/LWORD | 8 bytes | 64-bit signed/unsigned |
| REAL | 4 bytes | 32-bit float |
| LREAL | 8 bytes | 64-bit float |
| STRING | Variable | Null-terminated text |

**Byte Order**: S7 and Omron use big-endian; Logix and Beckhoff use little-endian. WarLogix handles this automatically.

## PLC-Specific Notes

### Siemens S7

- **Addressing**: `DB<n>.DB<type><offset>` (e.g., `DB1.DBD0` for DINT at byte 0)
- **Types**: DBX=bit, DBB=byte, DBW=word, DBD=dword
- **Arrays**: `DB1.0[10]` for 10 elements
- **TIA Portal**: Enable PUT/GET access; disable optimized block access for DBs

### Beckhoff TwinCAT

- **AMS Net ID**: Typically `IP.1.1` (e.g., `192.168.1.100.1.1`)
- **AMS Ports**: 851 (TC3 Runtime 1), 852 (Runtime 2), 801 (TC2)
- **Symbols**: Dot notation (`MAIN.Variable`, `GVL.GlobalVar`)
- **Routes**: Add a static route in TwinCAT System Manager for WarLogix machine

### Omron FINS

- **Memory Areas**: DM (data), CIO (I/O), WR (work), HR (holding), AR (auxiliary)
- **Addressing**: `<Area><Word>[.Bit]` (e.g., `DM100`, `CIO0.5`)
- **Arrays**: `DM100[10]` for 10 words
- **Port**: Default UDP 9600

### Tag Aliases

For S7 and Omron PLCs, assign friendly names to addresses:
```yaml
- name: DB1.DBD0
  alias: ProductCount
  data_type: DINT
```

## Event Triggers

Triggers capture data snapshots when PLC conditions are met and publish to Kafka:

1. Monitor a trigger tag (e.g., `ProductReady`)
2. When condition met (rising edge), capture configured data tags
3. Publish JSON message to Kafka with timestamp and sequence number
4. Optionally write acknowledgment (1=success, -1=error) to PLC

**Condition operators**: `==`, `!=`, `>`, `<`, `>=`, `<=`

## Limitations

- Structs/UDTs are published as raw byte arrays (not decoded)
- BETA release - improvements ongoing

## License

Apache License 2.0

## Acknowledgements

- Pylogix / dmroeder - reference code for EtherNet/IP development

## Contributing

Contributions welcome! Please submit a Pull Request.
