# REST API Reference

The REST API exposes PLC data over HTTP for integration with other systems and debugging.   This can be useful if the other systems reside on a routable subnet and need a simple polling check, but the other publishing options are much more performant and should be preferred.   Namespaces are not used for the REST server since read and write requests are targeted at a specific WarLogix instance already.

<img width="1118" height="667" alt="image" src="https://github.com/user-attachments/assets/d7acb0d7-c245-48ea-abb0-a77956c4aaa6" />

## Configuration

```yaml
rest:
  enabled: true
  port: 8080
  host: 0.0.0.0    # Bind to all interfaces
```

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
  {"name": "ProductionMetrics", "enabled": true, "topic": "packs/production", "members": 4, "url": "/tagpack/ProductionMetrics"},
  {"name": "Alarm Pack", "enabled": false, "topic": "packs/alarms", "members": 8, "url": "/tagpack/Alarm%20Pack"}
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
