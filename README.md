# WarLogix


**WAR** stands for *"Whispers Across Realms"* - bridging the gap between industrial automation and modern IT infrastructure.

Factory floors speak their own languages: EtherNet/IP, S7comm, ADS, FINS. Meanwhile, your data platforms expect REST, MQTT, Kafka, and Redis. WarLogix translates between these worlds, letting you stream PLC data to dashboards, databases, and analytics pipelines without writing custom integration code.  No expensive middleware. No vendor lock-in. Just a single binary that runs anywhere.

At its heard WarLogix is a TUI (Text User Interface) gateway for industrial PLCs, and can connect to Allen-Bradley, Siemens, Beckhoff, and Omron PLCs and republish data via REST API, MQTT, Kafka, and Redis/Valkey.   It includes advanced features for grouping tags and publishing on trigger conditions, and is optimized for high performance.   It was originally designed as an "Edge" application to simplify pushing data out of NAT'd process networks, but is found to also work well in the server room for aggregating and monitoring data factory-wide.

> **BETA** - Allen-Bradley and Siemens support is well-tested. Beckhoff is stable but requires more testing, and Omron support is still experimental.

<img width="837" height="537" alt="image" src="https://github.com/user-attachments/assets/9a1794ae-725d-468d-820b-b80e96c09888" />


## Warnings

- **WarLogix is not a real-time control system** - See [Safety and Intended Use](docs/safety-and-intended-use.md)
- Write-back should only be used for acknowledgments on dedicated tags
- Never use WarLogix for safety-critical functions or machine control
- This software is provided without warranty - use at your own risk

## Quick Start

### Download

Pre-built binaries are available on the [Releases](https://github.com/yatesdr/warlogix/releases) page for Linux, macOS, and Windows.

### Build from Source (Optional)

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


## Documentation

### Getting Started
- [User Interface Guide](docs/ui-tabs.md) - TUI tabs and keyboard shortcuts
- [Configuration Reference](docs/configuration.md) - Config file format and options
- [PLC Setup Guide](docs/plc-setup.md) - PLC-specific setup and troubleshooting

### Services
- [REST API](docs/rest-api.md) - HTTP endpoints for tag values and writes
- [MQTT](docs/mqtt.md) - Topics, QoS settings, and write-back
- [Valkey/Redis](docs/valkey.md) - Keys, Pub/Sub, and write-back queue
- [Kafka](docs/kafka.md) - Topics, authentication, and batching

### Advanced Features
- [Triggers](docs/triggers.md) - Event-driven data capture to MQTT and Kafka
- [TagPacks](docs/tagpacks.md) - Aggregate tags for atomic publishing
- [Multi-Instance Deployment](docs/multi-instance.md) - Namespace isolation for multiple sites
- [Data Types](docs/data-types.md) - Types, byte order, and UDT support
- [Performance Guide](docs/performance.md) - Optimization and benchmarking

### Reference
- [Safety and Intended Use](docs/safety-and-intended-use.md) - **Important limitations and proper use of write-back**
- [Developer Guide](docs/developer.md) - Using drivers in your own Go applications

## Features

- **Multi-PLC Support** - Connect to multiple PLCs from different vendors simultaneously
- **Tag Browser** - Browse and select tags with real-time value updates
- **UDT Support** - Automatic structure unpacking with change detection filtering
- **Health Monitoring** - Periodic health publishing (REST, MQTT, Valkey, Kafka)
- **REST API** - HTTP endpoints for tag values and writes
- **MQTT** - Publish tags with optional write-back
- **Valkey/Redis** - Key storage with Pub/Sub and write-back queue
- **Kafka** - High-throughput tag changes and event triggers
- **TagPacks** - Group tags within a single PLC or across multiple PLCs for atomic publishing, useful for aggregating related data for upstream IT processes
- **Triggers** - Event-driven data capture with MQTT (QoS 2) and Kafka publishing
- **Daemon Mode** - Background service with SSH access
- **High Performance** - Batched reads, optimized publishing, 100K+ messages/sec

## Supported PLCs

| Family | Models | Tag Discovery | Support Level |
|--------|--------|---------------|---------------|
| Allen-Bradley | ControlLogix, CompactLogix, Micro800 | Automatic | Well tested |
| Siemens | S7-300/400/1200/1500 | Manual | Moderately tested |
| Beckhoff | TwinCAT 2/3 | Automatic | Moderately tested |
| Omron | CJ/CS/CP/NJ/NX Series | Manual | Experimental |

## Performance

WarLogix is designed for high-throughput industrial data streaming with batched PLC reads, change filtering, and efficient broker publishing.

### Republishing Throughput

Simulated publishing test with 50 PLCs × 100 tags (5,000 total tags) on localhost:

| Broker | Confirmed Delivery | Implementation |
|--------|-------------------|----------------|
| Kafka | 290,000+ msg/s | Batched async |
| Valkey | 45,000+ msg/s | Synchronous |
| MQTT | 32,000+ msg/s | Synchronous QoS 1 |

### PLC Read Performance

| PLC Family | Batching | Typical Throughput |
|------------|----------|-------------------|
| Allen-Bradley Logix | Yes (scalars) | 500-2,000 tags/sec |
| Siemens S7 | Yes (aggressive) | 300-1,500 tags/sec |
| Beckhoff ADS | Yes (SumUp Read) | 1,000-5,000 tags/sec |
| Omron FINS | No | 50-200 tags/sec |

Run `warlogix --stress-test-republishing` to benchmark your system.

## Keyboard Shortcuts

| Tab | Key | Action |
|-----|-----|--------|
| Global | `P/B/T/G/E/M/V/K/D` | Jump to tab |
| Global | `Shift+Tab` | Cycle tabs |
| Global | `N` | Configure namespace |
| Global | `F6` | Cycle themes |
| Global | `?` | Help |
| Global | `Q` | Quit |
| PLCs | `d/a/e/x` | Discover/Add/Edit/Remove |
| PLCs | `c/C/i` | Connect/Disconnect/Info |
| Browser | `Space/w/i` | Toggle publish/writable/ignore |
| Browser | `/` then `c` | Filter / Clear filter |
| TagPacks | `a/x` | Add/Remove (context-sensitive) |
| Triggers | `a/x/e` | Add/Remove/Edit (context-sensitive) |
| Triggers | `s/S/T` | Start/Stop/Test fire |
| Services | `a/e/x/c/C` | Add/Edit/Remove/Connect/Disconnect |

## Command Line Options

```
--config <path>              Path to config file (default: ~/.warlogix/config.yaml)
--log <path>                 Write debug messages to a file
--log-debug                  Enable verbose protocol debugging to debug.log
-d                           Daemon mode (serve TUI over SSH)
-p <port>                    SSH port for daemon mode (default: 2222)
--ssh-password <pw>          SSH password authentication
--ssh-keys <path>            Path to authorized_keys file
--stress-test-republishing   Stress test Kafka, MQTT, and Valkey throughput
--version                    Show version
```

## Daemon Mode

Run as a background service with SSH access:

```bash
./warlogix -d -p 2222 --ssh-password "secret"
ssh -p 2222 localhost
```

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
