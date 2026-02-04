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
      - name: DB1.0
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

### Byte Order Handling

Different PLC families use different byte orders for multi-byte values:

| PLC Family | Native Byte Order |
|------------|-------------------|
| Siemens S7 | Big-endian |
| Omron FINS | Big-endian |
| Allen-Bradley Logix | Little-endian |
| Beckhoff TwinCAT | Little-endian |

**Known data types** (BOOL, INT, DINT, REAL, STRING, etc.) are automatically converted to the correct values regardless of the PLC's native byte order. You'll see the same numeric value whether it comes from an S7 or a Logix PLC.

**Unknown data types** (structs, UDTs, and unrecognized types) are returned as raw byte arrays in the PLC's native byte order. If you need to decode these manually:
- For S7 and Omron: bytes are in big-endian order (most significant byte first)
- For Logix and Beckhoff: bytes are in little-endian order (least significant byte first)

Example of a raw byte array for a 4-byte integer value of 0x12345678:
- Big-endian (S7/Omron): `[18, 52, 86, 120]` (0x12, 0x34, 0x56, 0x78)
- Little-endian (Logix/Beckhoff): `[120, 86, 52, 18]` (0x78, 0x56, 0x34, 0x12)

## PLC Configuration Guide

### Allen-Bradley ControlLogix/CompactLogix

**PLC-Side Setup:**
- No special configuration required - EtherNet/IP is enabled by default
- Ensure the PLC has an IP address configured (via RSLogix/Studio 5000 or DHCP)
- The EtherNet/IP port (TCP 44818) must be accessible from WarLogix

**WarLogix Configuration:**
- `address`: PLC IP address
- `slot`: CPU slot number (typically 0 for CompactLogix, varies for ControlLogix)
- Tags are discovered automatically; select which to publish in the Tag Browser

**Troubleshooting:**
- Verify network connectivity with ping
- Check that no firewall blocks port 44818
- For ControlLogix in remote chassis, ensure routing path is correct

### Siemens S7-1200/1500

**PLC-Side Setup (TIA Portal):**
1. Open your project in TIA Portal
2. Select the PLC and open **Properties** > **Protection & Security**
3. Enable **Permit access with PUT/GET communication from remote partner**
4. For each Data Block you want to access:
   - Open DB properties > **Attributes**
   - Uncheck **Optimized block access** (enables absolute addressing)
5. Download the project to the PLC

**WarLogix Configuration:**
- `address`: PLC IP address
- `slot`: Rack/slot - use `0` for S7-1200/1500 integrated CPU
- `family`: `s7`
- Tags must be configured manually with byte offsets

**Addressing Format:**
- `DB<n>.<offset>` with `data_type` field (e.g., `DB1.0` with `data_type: DINT`)
- Bit access: `DB<n>.<offset>.<bit>` (e.g., `DB1.4.0` for bit 0 at byte 4)
- Other areas: `I<offset>`, `Q<offset>`, `M<offset>` (inputs, outputs, markers)
- Arrays: `DB1.0[10]` for 10 elements starting at byte 0

**Troubleshooting:**
- "Connection refused" - PUT/GET not enabled or wrong IP
- "Access denied" - DB has optimized access enabled
- Wrong values - check byte offsets match your DB layout

### Siemens S7-300/400

**PLC-Side Setup:**
- No special configuration typically required
- Ensure the CP (Communications Processor) or integrated Ethernet port has an IP address

**WarLogix Configuration:**
- `address`: PLC IP address
- `slot`: CPU slot (typically `2` for S7-300, varies for S7-400)
- `rack`: Rack number (typically `0`)

### Beckhoff TwinCAT

**PLC-Side Setup:**
1. **Configure AMS Net ID**: In TwinCAT System Manager, note your PLC's AMS Net ID (usually `<IP>.1.1`)
2. **Add a Route** for the WarLogix machine:
   - Open TwinCAT System Manager > SYSTEM > Routes
   - Add Static Route:
     - **Name**: WarLogix (or any identifier)
     - **AMS Net ID**: Use the WarLogix machine's IP + `.1.1` (e.g., `192.168.1.50.1.1`)
     - **Address**: WarLogix machine IP address
     - **Transport**: TCP/IP
3. **Firewall**: Ensure port 48898 (ADS) is open for TCP traffic
4. **PLC must be in RUN mode** for symbol access

**WarLogix Configuration:**
- `address`: PLC IP address
- `ams_net_id`: PLC's AMS Net ID (e.g., `192.168.1.100.1.1`)
- `ams_port`: Runtime port - `851` for TwinCAT 3 Runtime 1, `801` for TwinCAT 2
- `family`: `beckhoff`
- Tags are discovered automatically from the symbol table

**Symbol Naming:**
- `MAIN.Variable` - Variable in MAIN program
- `GVL.GlobalVar` - Global Variable List
- `FB_Instance.Member` - Function block members

**Troubleshooting:**
- "No route" - Add a route in TwinCAT for the WarLogix machine
- "Port not found" - Check AMS port matches your runtime (851 vs 801)
- No symbols - Ensure PLC is in RUN mode and project is activated

### Omron FINS (CJ/CS/CP/NJ/NX Series)

**PLC-Side Setup:**
1. **Configure IP Address**: Set via CX-Programmer, Sysmac Studio, or rotary switches
2. **FINS Port**: Default UDP 9600 (usually no change needed)
3. **Node Address**: Configure in PLC settings; often matches last octet of IP address
4. **Firewall**: Ensure UDP port 9600 is open

**WarLogix Configuration:**
- `address`: PLC IP address
- `fins_port`: UDP port (default `9600`)
- `fins_node`: PLC's FINS node number (often `0` or last IP octet)
- `fins_network`: FINS network number (usually `0` for local)
- `fins_unit`: CPU unit number (usually `0`)
- `family`: `omron`
- Tags must be configured manually

**Addressing Format:**
- **Memory Areas**: DM (data), CIO (I/O), WR (work), HR (holding), AR (auxiliary)
- Word access: `DM100`, `CIO50`, `HR10`
- Bit access: `DM100.5` (bit 5 of DM100)
- Arrays: `DM100[10]` for 10 consecutive words

**Troubleshooting:**
- Timeout errors - Check IP, port, and that PLC is powered on
- Wrong node - Verify FINS node address matches PLC configuration
- No response - Ensure UDP 9600 is not blocked by firewall

### Tag Aliases

For S7 and Omron PLCs (which use address-based tags), assign friendly names:
```yaml
- name: DB1.0
  alias: ProductCount
  data_type: DINT
```
The alias appears in MQTT/Valkey/Kafka messages instead of the raw address.

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

This project builds on excellent open source libraries:

**PLC Communication:**
- [gos7](https://github.com/robinson/gos7) - Siemens S7 protocol implementation
- [fins](https://github.com/xiaotushaoxia/fins) - Omron FINS/UDP protocol
- [pylogix](https://github.com/dmroeder/pylogix) - Reference for Allen-Bradley EtherNet/IP

**Infrastructure:**
- [paho.mqtt.golang](https://github.com/eclipse/paho.mqtt.golang) - Eclipse MQTT client
- [go-redis](https://github.com/redis/go-redis) - Redis/Valkey client
- [kafka-go](https://github.com/segmentio/kafka-go) - Apache Kafka client

**User Interface:**
- [tview](https://github.com/rivo/tview) - Terminal UI framework
- [tcell](https://github.com/gdamore/tcell) - Terminal cell library

**Utilities:**
- [yaml.v3](https://github.com/go-yaml/yaml) - YAML parsing

## Contributing

Contributions welcome! Please submit a Pull Request.
