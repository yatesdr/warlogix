# WarLogix

A TUI (Text User Interface) gateway for industrial PLCs. Connect to Allen-Bradley, Siemens, Beckhoff, and Omron PLCs and republish data via REST API, MQTT, Kafka, and Redis/Valkey.

> **BETA** - Allen-Bradley and Siemens support is well-tested. Beckhoff and Omron support are works in progress.

<img width="837" height="537" alt="image" src="https://github.com/user-attachments/assets/9a1794ae-725d-468d-820b-b80e96c09888" />


## Why WarLogix?

**WAR** stands for *"Whispers Across Realms"* - bridging the gap between industrial automation and modern IT infrastructure.

Factory floors speak their own languages: EtherNet/IP, S7comm, ADS, FINS. Meanwhile, your data platforms expect REST, MQTT, Kafka, and Redis. WarLogix translates between these worlds, letting you stream PLC data to dashboards, databases, and analytics pipelines without writing custom integration code.

**Use cases:**
- Real-time production monitoring and OEE dashboards
- Historical data collection for analytics and ML
- Event-driven alerts and notifications
- Bidirectional control from IT systems to PLCs
- Multi-vendor PLC consolidation into unified data streams

No expensive middleware. No vendor lock-in. Just a single binary that runs anywhere.

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
./warlogix
```

Configuration is stored at `~/.warlogix/config.yaml` and created automatically on first run.

### Navigation

- `Shift+Tab` - Switch tabs
- `?` - Help
- `Q` - Quit

## Supported PLCs

| Family | Models | Tag Discovery | Support Level |
|--------|--------|---------------|---------------|
| Allen-Bradley | ControlLogix, CompactLogix, Micro800 | Automatic | Well tested |
| Siemens | S7-300/400/1200/1500 | Manual | Moderately tested |
| Beckhoff | TwinCAT 2/3 | Automatic | Moderately tested |
| Omron | CJ/CS/CP/NJ/NX Series | Manual | Experimental |

**Note:** Allen-Bradley Logix has the most complete support. Siemens and Beckhoff are functional but less tested. Omron FINS support is experimental and may have bugs.

Press `d` to discover PLCs on your network, or `a` to add manually.

## Keyboard Shortcuts

| Tab | Key | Action |
|-----|-----|--------|
| Global | `Shift+Tab` | Switch tabs |
| Global | `F6` | Cycle themes |
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

## Documentation

- [Configuration Reference](docs/configuration.md) - Config file format and options
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

## Acknowledgements

This project builds on excellent open source libraries:

**PLC Communication:**
- [gos7](https://github.com/robinson/gos7) - Siemens S7 protocol
- [fins](https://github.com/xiaotushaoxia/fins) - Omron FINS/UDP protocol
- [pylogix](https://github.com/dmroeder/pylogix) - Allen-Bradley EtherNet/IP reference

**Infrastructure:**
- [paho.mqtt.golang](https://github.com/eclipse/paho.mqtt.golang) - MQTT client
- [go-redis](https://github.com/redis/go-redis) - Redis/Valkey client
- [kafka-go](https://github.com/segmentio/kafka-go) - Kafka client

**User Interface:**
- [tview](https://github.com/rivo/tview) - Terminal UI framework
- [tcell](https://github.com/gdamore/tcell) - Terminal cell library
- [gliderlabs/ssh](https://github.com/gliderlabs/ssh) - SSH server
- [creack/pty](https://github.com/creack/pty) - PTY handling
