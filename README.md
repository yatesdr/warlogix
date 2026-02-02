# WarLogix

A TUI (Text User Interface) gateway application for Allen-Bradley/Rockwell Automation ControlLogix and CompactLogix PLCs. Browse tags, monitor values in real-time, and republish data via REST API and MQTT.

WarLogix Beta release is still a little buggy, but most issues can be resolved by restarting the application if something weird is found.

## The name

WAR stands for "whispers across realms" - this application is intended to provide a gateway between industrial and IT applications - specifically Logix PLC's and REST / MQTT formats.   Kafka is on the feature list for addition in the near future.

## Features

- **PLC Discovery**: Automatically discover PLCs on your network via UDP broadcast or direct TCP connection
- **Tag Browser**: Browse controller and program-scoped tags with real-time value updates
- **REST API**: Expose PLC tag values via a REST API for integration with other systems
- **MQTT Publishing**: Publish tag values to MQTT brokers on change, with retained messages
- **MQTT Write-back**: Write to PLC tags via MQTT with proper type conversion and validation
- **Multi-PLC Support**: Connect to and manage multiple PLCs simultaneously
- **Configuration Persistence**: Save your PLC connections, tag selections, and settings to YAML

## Limitations

- Does not currently decode structs or UDT's.
- No auth for MQTT or configuring settings.
- Only limited testing has been done on one ControlLogix PLC for the basic types.
- This is a BETA release and will be improved as bugs are identified and remediated.

## Warnings

- This software allows reading and writing to industrial PLC's, which can present hazards if done poorly.
- No warranty or liability is assumed - use at your own risk.

## Supported PLCs

- Allen-Bradley ControlLogix (1756 series)
- Allen-Bradley CompactLogix (1769 series)
- Other PLCs supporting EtherNet/IP and CIP protocols

## Basic Usage

Download the binary, or build from source, and run the application.

- Navigate between tabs by pressing Shift-Tab.
- Use the hot-keys to add one or more PLC's, edit settings, browse tags, and configure REST and MQTT re-publishing.
- By default, no tags will be synced.   Use the Tag Browser tab to select tags to publish and set the status.
- If you are on the same switch as the PLC you may be able to discover available PLC's by pressing 'd', but in many situations the UDP discovery is limited by broadcast domain and you will need to add the PLC by the IP Address.

<img width="885" height="582" alt="image" src="https://github.com/user-attachments/assets/660d1c0f-bce3-47b8-aa2f-5451f403ae70" />
<img width="364" height="220" alt="image" src="https://github.com/user-attachments/assets/9063a2a5-4aa6-4646-abbc-9ca39d90e958" />
<pre>

  
</pre>

- To add tags for read / republish you can select them in the tag browser (Shift-Tab to tab to it)
<img width="882" height="576" alt="image" src="https://github.com/user-attachments/assets/9cf6c677-0a4b-43f7-85a0-9e24bd3131b5" />
<pre>

  
</pre>

- The REST endpoint is read-only, but useful for polling state.
<img width="885" height="578" alt="image" src="https://github.com/user-attachments/assets/f016aa2b-af67-42c1-a0b9-42949fbb99d8" />
<pre>

  
</pre>

- The MQTT re-publisher can republish to any accessible MQTT broker, but does not currently support auth.  This is the primary way to move data into the IT world as it can punch out of the typical machine network onto the IT side with a proper firewall config.
- MQTT protocol offers write-back for write-enabled tags with a properly formatted write request (see more below).  This is only tested on basic types and should not be used as part of a control system.   It is intended for ack / clear requests to the PLC.
<img width="887" height="579" alt="image" src="https://github.com/user-attachments/assets/20bbab85-bf11-4d58-a352-058acab28197" />
<pre>

  
</pre>


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

## Configuration

Configuration is stored in YAML format at `~/.warlogix/config.yaml` - you do not generally need to manually edit this:

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

## Acknowledgements

 - Pylogix / dmroeder - reference code was used to help identify and troubleshoot throughout the development process.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
