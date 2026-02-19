# REST API Reference

The REST API exposes PLC data over HTTP for integration with other systems and debugging.   This can be useful if the other systems reside on a routable subnet and need a simple polling check, but the other publishing options are much more performant and should be preferred.   Namespaces are not used for the REST server since read and write requests are targeted at a specific WarLink instance already.

<img width="1118" height="667" alt="image" src="https://github.com/user-attachments/assets/d7acb0d7-c245-48ea-abb0-a77956c4aaa6" />

## Configuration

The REST API is part of the consolidated web server. Configure it under the `web:` key:

```yaml
web:
  enabled: true
  port: 8080
  host: 0.0.0.0    # Bind to all interfaces
  api:
    enabled: true   # Enable REST API endpoints
```

The API can also be enabled from the command line:

```bash
./warlink --admin-user admin --admin-pass yourpassword
```

Use `--no-api` to disable the REST API while keeping the web UI, or `--no-webui` to disable the browser UI while keeping the API.

## Endpoints

### List PLCs

```
GET /
```

Returns all configured PLCs with their connection status.

**Response:**
```json
[
  {
    "name": "MainPLC",
    "address": "192.168.1.100",
    "slot": 0,
    "family": "logix",
    "status": "connected"
  }
]
```

### PLC Details

```
GET /{plc}
```

Returns PLC details including device identity (when connected).

**Response:**
```json
{
  "name": "MainPLC",
  "address": "192.168.1.100",
  "slot": 0,
  "family": "logix",
  "status": "connected",
  "identity": {
    "vendor": "Rockwell Automation/Allen-Bradley",
    "product_type": "Programmable Logic Controller",
    "product_name": "1769-L33ER CompactLogix 5370",
    "serial": 12345678,
    "revision": "32.11"
  }
}
```

### List Programs (Logix only)

```
GET /{plc}/programs
```

Returns list of programs in the PLC.

**Response:**
```json
["MainProgram", "Alarms", "Communications"]
```

### All Tags

```
GET /{plc}/tags
```

Returns all enabled tags with current values.

**Response:**
```json
[
  {
    "plc": "MainPLC",
    "name": "Counter",
    "value": 42,
    "type": "DINT",
    "timestamp": "2024-01-15T10:30:00Z"
  },
  {
    "plc": "MainPLC",
    "name": "Temperature",
    "alias": "TempSensor1",
    "value": 72.5,
    "type": "REAL",
    "timestamp": "2024-01-15T10:30:00Z"
  }
]
```

### Single Tag

```
GET /{plc}/tags/{tag}
```

Returns a single tag value. Tag names with slashes are supported.

**Response:**
```json
{
  "plc": "MainPLC",
  "name": "Program:MainProgram.Counter",
  "value": 42,
  "type": "DINT",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### All Known Tags (Discovery)

```
GET /{plc}/all-tags
```

Returns every tag known to warlink for a PLC — discovered, configured, or both — deduplicated. Includes config state and current value for enabled tags. Useful for external applications that need to discover available tags and then selectively enable them.

**Response:**
```json
[
  {
    "name": "Counter",
    "type": "DINT",
    "configured": true,
    "enabled": true,
    "writable": false,
    "value": 42
  },
  {
    "name": "UnconfiguredTag",
    "type": "REAL",
    "configured": false,
    "enabled": false,
    "writable": true
  },
  {
    "name": "DisabledTag",
    "type": "INT",
    "configured": true,
    "enabled": false,
    "writable": false,
    "no_mqtt": true
  }
]
```

**Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Tag name |
| `type` | string | Data type (from discovery or config) |
| `configured` | bool | Whether this tag has a config entry |
| `enabled` | bool | Whether the tag is enabled for polling |
| `writable` | bool | Whether the tag is writable |
| `no_rest` | bool | Omitted when false. Excludes tag from `GET /{plc}/tags` |
| `no_mqtt` | bool | Omitted when false. Excludes tag from MQTT publishing |
| `no_kafka` | bool | Omitted when false. Excludes tag from Kafka publishing |
| `no_valkey` | bool | Omitted when false. Excludes tag from Valkey publishing |
| `value` | any | Current value (only present when tag is enabled and has a value) |

### Update Tag Config

```
PATCH /{plc}/tags/{tag}
```

Updates a single tag's configuration flags. All fields are optional — only provided fields are changed. If the tag does not have a config entry, one is auto-created.

**Request:**
```json
{
  "enabled": true,
  "writable": false,
  "no_rest": false,
  "no_mqtt": false,
  "no_kafka": false,
  "no_valkey": false
}
```

**Response:**
```json
{
  "status": "updated"
}
```

**Example — enable a discovered tag for polling:**
```bash
curl -X PATCH http://localhost:8080/api/MainPLC/tags/DiscoveredTag \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'
```

### PLC Health

```
GET /{plc}/health
```

Returns PLC connection health status.

**Response:**
```json
{
  "plc": "MainPLC",
  "online": true,
  "status": "connected",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

**Status values:** `connected`, `connecting`, `disconnected`, `disabled`, `error`

### List TagPacks

```
GET /tagpack
```

Returns all configured TagPacks.

**Response:**
```json
[
  {"name": "ProductionMetrics", "enabled": true, "members": 4, "url": "/tagpack/ProductionMetrics"},
  {"name": "Alarm Pack", "enabled": false, "members": 8, "url": "/tagpack/Alarm%20Pack"}
]
```

### Get TagPack

```
GET /tagpack/{name}
```

Returns current values for all tags in a TagPack.

**Response:**
```json
{
  "name": "ProductionMetrics",
  "timestamp": "2024-01-15T10:30:00.123Z",
  "tags": {
    "MainPLC.Counter": {
      "value": 1234,
      "type": "DINT",
      "plc": "MainPLC"
    },
    "MainPLC.Temperature": {
      "value": 72.5,
      "type": "REAL",
      "plc": "MainPLC"
    }
  }
}
```

### Server-Sent Events (SSE)

```
GET /events
```

Streams real-time PLC data as Server-Sent Events. Provides tag value changes, tagpack publishes, PLC connection status, and health updates. No authentication required.

**Query Parameters:**

| Parameter | Description | Example |
|-----------|-------------|---------|
| `types` | Comma-separated event type filter. Omit to receive all types. | `?types=value-change,health` |
| `plc` | Filter PLC-specific events to a single PLC. Non-PLC events (e.g. `tagpack`) pass through. | `?plc=MainPLC` |

**Event Types:**

| Event | Trigger | Frequency |
|-------|---------|-----------|
| `value-change` | Tag value changes during poll cycle | Per changed tag, every poll (~1s) |
| `tagpack` | TagPack debounced publish | 250ms after member tag changes |
| `status-change` | PLC connects, disconnects, or errors | On connection state transitions |
| `health` | PLC health check | Every 10s (2s initial delay) |

**Initial event** (sent on connect):
```
event: connected
data: {"id":"api-1234567890"}
```

**`value-change` event:**
```json
{"plc": "MainPLC", "tag": "Counter", "value": 42, "type": "DINT"}
```

**`tagpack` event:**
```json
{
  "name": "ProductionMetrics",
  "timestamp": "2024-01-15T10:30:00.123Z",
  "tags": {
    "MainPLC.Counter": {"value": 42, "type": "DINT", "plc": "MainPLC"},
    "MainPLC.Temperature": {"value": 72.5, "type": "REAL", "plc": "MainPLC"}
  }
}
```

**`status-change` event:**
```json
{
  "plc": "MainPLC",
  "status": "connected",
  "tagCount": 24,
  "productName": "1769-L33ER CompactLogix 5370",
  "vendor": "Rockwell Automation/Allen-Bradley",
  "connectionMode": "Connected"
}
```

**`health` event:**
```json
{
  "plc": "MainPLC",
  "driver": "logix",
  "online": true,
  "status": "connected",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

A keepalive comment (`: keepalive`) is sent every 30 seconds to prevent proxy timeouts.

### Write Tag

```
POST /{plc}/write
```

Write a value to a tag. Tag must be marked as `writable: true` in configuration.

**Request:**
```json
{
  "plc": "MainPLC",
  "tag": "Program:MainProgram.Counter",
  "value": 100
}
```

**Success Response:**
```json
{
  "plc": "MainPLC",
  "tag": "Program:MainProgram.Counter",
  "value": 100,
  "success": true,
  "timestamp": "2024-01-15T10:30:05Z"
}
```

**Error Response:**
```json
{
  "plc": "MainPLC",
  "tag": "Program:MainProgram.Counter",
  "value": 100,
  "success": false,
  "error": "tag is not writable",
  "timestamp": "2024-01-15T10:30:05Z"
}
```

## Error Codes

| HTTP Status | Meaning |
|-------------|---------|
| 400 | Invalid JSON or PLC name mismatch |
| 403 | Tag is not marked as writable |
| 404 | PLC or tag not found |
| 405 | Method not allowed |
| 500 | Write failed or timeout |
| 503 | PLC not connected |

## Value Type Handling

JSON values are automatically converted to the tag's data type:

| JSON Type | PLC Types |
|-----------|-----------|
| boolean | BOOL |
| integer | SINT, INT, DINT, LINT, USINT, UINT, UDINT, ULINT |
| number | REAL, LREAL |
| string | STRING |
| array | Arrays of any supported type |

## Examples

**cURL - Read all tags:**
```bash
curl http://localhost:8080/MainPLC/tags
```

**cURL - Read single tag:**
```bash
curl http://localhost:8080/MainPLC/tags/Program:MainProgram.Counter
```

**cURL - Write tag:**
```bash
curl -X POST http://localhost:8080/MainPLC/write \
  -H "Content-Type: application/json" \
  -d '{"plc":"MainPLC","tag":"Counter","value":100}'
```

**cURL - Check health:**
```bash
curl http://localhost:8080/MainPLC/health
```

**cURL - Stream all SSE events:**
```bash
curl -N http://localhost:8080/api/events
```

**cURL - List all known tags (including unconfigured):**
```bash
curl http://localhost:8080/api/MainPLC/all-tags
```

**cURL - Enable a discovered tag:**
```bash
curl -X PATCH http://localhost:8080/api/MainPLC/tags/NewTag \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'
```

**cURL - Stream only value changes from a specific PLC:**
```bash
curl -N "http://localhost:8080/api/events?types=value-change&plc=MainPLC"
```
