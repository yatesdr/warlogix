# WarLogix

A TUI (Text User Interface) gateway for industrial PLCs. Connect to Allen-Bradley, Siemens, Beckhoff, and Omron PLCs and republish data via REST API, MQTT, Kafka, and Redis/Valkey.

> **BETA** - Allen-Bradley and Siemens support is well-tested. Beckhoff and Omron support are works in progress.

<img width="961" height="579" alt="WarLogix TUI" src="https://github.com/user-attachments/assets/4bdbc47e-6ca1-41a1-992d-c46356d6415c" />

## Features

- **Multi-PLC Support** - Connect to multiple PLCs from different vendors simultaneously
- **Tag Browser** - Browse and select tags with real-time value updates
- **UDT Support** - Automatic structure unpacking with change detection filtering
- **Health Monitoring** - Periodic health publishing (REST, MQTT, Valkey, Kafka)
- **REST API** - HTTP endpoints for tag values and writes
- **MQTT** - Publish tags with optional write-back
- **Valkey/Redis** - Key storage with Pub/Sub and write-back queue
- **Kafka** - Tag changes and event triggers
- **Daemon Mode** - Background service with SSH access

## Quick Start

### Install

```bash
git clone https://github.com/yatesdr/warlogix.git
cd warlogix
go build -o warlogix ./cmd/warlogix
```

### Run

```bash
./warlogix                    # Uses ~/.warlogix/config.yaml
./warlogix -config myconfig.yaml
```

### Basic Navigation

- `Shift+Tab` - Switch tabs
- `?` - Help
- `Q` - Quit

## Adding PLCs

Press `a` in the PLCs tab or use `d` to discover PLCs on your network.

### Supported PLCs

| Family | Discovery | Config |
|--------|-----------|--------|
| Allen-Bradley Logix | Automatic | `family: logix` |
| Allen-Bradley Micro800 | Automatic | `family: micro800` |
| Siemens S7 | Manual tags | `family: s7` |
| Beckhoff TwinCAT | Automatic | `family: beckhoff` |
| Omron FINS | Manual tags | `family: omron` |

## Configuration Basics

Config file: `~/.warlogix/config.yaml`

```yaml
plcs:
  # Allen-Bradley (automatic tag discovery)
  - name: MainPLC
    address: 192.168.1.100
    family: logix
    slot: 0
    enabled: true              # Auto-connect on startup
    health_check_enabled: true # Publish health (default: true)
    tags:
      - name: Program:MainProgram.Counter
        enabled: true          # Publish this tag
        writable: true         # Allow writes
      - name: Program:MainProgram.MachineStatus
        enabled: true
        ignore_changes: [Timestamp, HeartbeatCount]  # Ignore volatile UDT members

  # Siemens S7 (manual tags with data_type)
  - name: SiemensPLC
    address: 192.168.1.101
    family: s7
    slot: 0
    tags:
      - name: DB1.0            # Address format: DB<n>.<offset>
        alias: ProductCount    # Friendly name for publishing
        data_type: DINT        # Required for S7
        enabled: true
        writable: true

  # Omron (manual tags)
  - name: OmronPLC
    address: 192.168.1.102
    family: omron
    fins_port: 9600
    tags:
      - name: DM100            # Memory area + address
        alias: MotorSpeed
        data_type: DINT
        enabled: true

rest:
  enabled: true
  port: 8080

mqtt:
  - name: Broker1
    enabled: true
    broker: localhost
    port: 1883
    root_topic: factory

valkey:
  - name: Redis1
    enabled: true
    address: localhost:6379
    factory: factory
    publish_changes: true
    enable_writeback: true

poll_rate: 1s
```

## Tag Configuration

| Field | Description |
|-------|-------------|
| `name` | Tag name or address |
| `alias` | Friendly name (for S7/Omron address-based tags) |
| `data_type` | Required for S7/Omron: BOOL, INT, DINT, REAL, STRING, etc. |
| `enabled` | Enable publishing |
| `writable` | Allow write operations |
| `ignore_changes` | UDT members to ignore for change detection |

### S7 Addressing

- `DB1.0` - Data block 1, byte 0
- `DB1.4.0` - Bit 0 at byte 4
- `DB1.0[10]` - Array of 10 elements
- `I0`, `Q0`, `M0` - Inputs, outputs, markers

### Omron Addressing

- `DM100` - Data memory
- `CIO50` - Core I/O
- `DM100.5` - Bit access
- `DM100[10]` - Array

## Keyboard Shortcuts

| Tab | Key | Action |
|-----|-----|--------|
| Global | `Shift+Tab` | Switch tabs |
| Global | `?` | Help |
| Global | `Q` | Quit |
| PLCs | `d/a/e/r` | Discover/Add/Edit/Remove |
| PLCs | `c/C/i` | Connect/Disconnect/Info |
| Browser | `/` | Filter tags |
| Browser | `Space` | Toggle publish |
| Browser | `w` | Toggle writable |
| Browser | `i` | Toggle ignore (UDT members) |
| MQTT/Valkey/Kafka | `a/e/r/c/C` | Add/Edit/Remove/Connect/Disconnect |
| Triggers | `a/e/r/t/x` | Add/Edit/Remove/Add tag/Remove tag |
| Triggers | `s/S/T` | Start/Stop/Test |

## Daemon Mode

Run as a background service with SSH access:

```bash
./warlogix -d -p 2222 --ssh-password "secret"
ssh -p 2222 localhost
```

See [detailed documentation](docs/) for more options.

## Documentation

- [Configuration Reference](docs/configuration.md) - Complete config.yaml options
- [PLC Setup Guide](docs/plc-setup.md) - PLC-specific setup and troubleshooting
- [REST API](docs/rest-api.md) - HTTP endpoints
- [MQTT](docs/mqtt.md) - Topics and write-back
- [Valkey/Redis](docs/valkey.md) - Keys, Pub/Sub, write-back queue
- [Kafka](docs/kafka.md) - Topics and authentication
- [Triggers](docs/triggers.md) - Event-driven data capture
- [Data Types](docs/data-types.md) - Types, byte order, UDT support

## Warnings

- This software reads/writes to industrial PLCs - use caution
- No warranty - use at your own risk

## License

Apache License 2.0
