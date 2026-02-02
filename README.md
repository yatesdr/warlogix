# Warlogix

A TUI (Text User Interface) gateway application for Allen-Bradley/Rockwell Automation ControlLogix and CompactLogix PLCs. Browse tags, monitor values in real-time, and republish data via REST API and MQTT.

## Features

- **PLC Discovery**: Automatically discover PLCs on your network via UDP broadcast or direct TCP connection
- **Tag Browser**: Browse controller and program-scoped tags with real-time value updates
- **REST API**: Expose PLC tag values via a REST API for integration with other systems
- **MQTT Publishing**: Publish tag values to MQTT brokers on change, with retained messages
- **MQTT Write-back**: Write to PLC tags via MQTT with proper type conversion and validation
- **Multi-PLC Support**: Connect to and manage multiple PLCs simultaneously
- **Configuration Persistence**: Save your PLC connections, tag selections, and settings to YAML

## Supported PLCs

- Allen-Bradley ControlLogix (1756 series)
- Allen-Bradley CompactLogix (1769 series)
- Other PLCs supporting EtherNet/IP and CIP protocols

## Installation

### From Source

```bash
git clone https://github.com/yourusername/warlogix.git
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

## Configuration

Configuration is stored in YAML format at `~/.warlogix/config.yaml`:

```yaml
plcs:
  - name: MainPLC
    address: 192.168.1.100
    slot: 0
    enabled: true
    tags:
      - name: Program:MainProgram.Counter
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

poll_rate: 1s
```

## REST API

When enabled, the REST API provides the following endpoints:

- `GET /plcs` - List all PLCs with status
- `GET /plcs/{name}` - PLC details and identity
- `GET /plcs/{name}/tags` - All tags with current values
- `GET /plcs/{name}/tags/{tagname}` - Single tag value

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

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
