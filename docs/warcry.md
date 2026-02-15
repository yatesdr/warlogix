# Warcry Connector

WarLink includes a built-in TCP server that streams PLC events to [warcry](https://github.com/derek/warcry) notification clients. The connector broadcasts tag changes, health status, and tagpack updates as newline-delimited JSON over a persistent TCP socket. Clients can also send discovery queries and replay missed events from a ring buffer after reconnect.

## Configuration

Add a `warcry` block to your WarLink config:

```yaml
warcry:
  enabled: true
  listen: "127.0.0.1:9999"
  buffer_size: 10000
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | `false` | Enable the warcry TCP server |
| `listen` | string | (required) | TCP listen address (e.g. `"127.0.0.1:9999"`, `":9999"`, `"0.0.0.0:9999"`) |
| `buffer_size` | int | `10000` | Number of events to retain in the ring buffer for replay |

The server only starts when both `enabled: true` and `listen` is non-empty. If the port is already in use, a warning is logged but WarLink continues to run normally.

## Wire Protocol

All communication uses **newline-delimited JSON** (NDJSON). Each message is a single JSON object followed by `\n`. Both directions (server-to-client and client-to-server) use the same framing.

The `type` field in every message identifies the message kind. The server never sends binary data.

## Connection Lifecycle

When a warcry client connects, the following sequence occurs automatically:

```
Client                                     WarLink
  │                                          │
  │──────── TCP connect ────────────────────▶│
  │                                          │
  │◀──────── config response ───────────────│  (1) namespace info
  │◀──────── snapshot ──────────────────────│  (2) all current tag values
  │                                          │
  │◀──────── live tag events ───────────────│  (3) ongoing stream
  │◀──────── live health events ────────────│
  │◀──────── live tagpack events ───────────│
  │                                          │
  │──────── list_tags request ─────────────▶│  (4) optional queries
  │◀──────── tag_list response ─────────────│
  │                                          │
  │──────── replay request ────────────────▶│  (5) optional replay
  │◀──────── buffered events ───────────────│
  │                                          │
```

1. **Config response** — Sent immediately with the WarLink namespace.
2. **Snapshot** — A single message containing all current tag values so the client has a complete picture without waiting for changes.
3. **Live stream** — Tag changes, health status, and tagpack updates are broadcast as they occur.
4. **Queries** — The client can request tag lists, pack lists, or config at any time.
5. **Replay** — The client can request buffered events since a timestamp to recover events missed during a disconnect.

## Server-to-Client Messages

### Tag Change

Sent when a PLC tag value changes. One message per tag.

```json
{
  "type": "tag",
  "plc": "MainPLC",
  "tag": "Program:MainProgram.Counter",
  "alias": "Counter",
  "address": "DB1.DBD100",
  "value": 42,
  "data_type": "DINT",
  "writable": true,
  "ts": "2024-01-15T10:30:00.123456789Z"
}
```

| Field | Type | Always Present | Description |
|-------|------|----------------|-------------|
| `type` | string | Yes | Always `"tag"` |
| `plc` | string | Yes | PLC name as configured in WarLink |
| `tag` | string | Yes | Tag name. For S7 PLCs, this is the alias (if set) or the memory address |
| `alias` | string | No | User-defined alias (only present if the tag has an alias) |
| `address` | string | No | Memory address (only present for address-based PLCs like S7, Omron FINS) |
| `value` | any | Yes | Current value (number, bool, string, array, or object for UDTs) |
| `data_type` | string | Yes | PLC data type: `BOOL`, `INT`, `DINT`, `REAL`, `STRING`, etc. |
| `writable` | bool | Yes | Whether write operations are allowed for this tag |
| `ts` | string | Yes | RFC 3339 timestamp with nanosecond precision (UTC) |

### Health Status

Sent every 10 seconds for each PLC with health checking enabled.

```json
{
  "type": "health",
  "plc": "MainPLC",
  "driver": "logix",
  "online": true,
  "status": "connected",
  "error": "connection timed out",
  "ts": "2024-01-15T10:30:00.123456789Z"
}
```

| Field | Type | Always Present | Description |
|-------|------|----------------|-------------|
| `type` | string | Yes | Always `"health"` |
| `plc` | string | Yes | PLC name |
| `driver` | string | Yes | PLC driver: `logix`, `s7`, `ads`, `omron` |
| `online` | bool | Yes | Whether the PLC is reachable |
| `status` | string | Yes | Connection state: `connected`, `connecting`, `disconnected`, `disabled`, `error` |
| `error` | string | No | Error message (only present when there is an error) |
| `ts` | string | Yes | RFC 3339 timestamp (UTC) |

### TagPack Update

Sent when a tagpack is published. The `data` field contains the raw tagpack JSON (same format published to MQTT/Kafka/Valkey).

```json
{
  "type": "tagpack",
  "name": "ProductionMetrics",
  "data": {
    "name": "ProductionMetrics",
    "timestamp": "2024-01-15T10:30:00Z",
    "tags": {
      "MainPLC.Counter": {
        "value": 42,
        "type": "DINT",
        "plc": "MainPLC"
      },
      "SecondaryPLC.Temperature": {
        "value": 72.5,
        "type": "REAL",
        "plc": "SecondaryPLC"
      }
    }
  },
  "ts": "2024-01-15T10:30:00.123456789Z"
}
```

| Field | Type | Always Present | Description |
|-------|------|----------------|-------------|
| `type` | string | Yes | Always `"tagpack"` |
| `name` | string | Yes | TagPack name |
| `data` | object | Yes | Full tagpack payload (same JSON as MQTT/Kafka) |
| `ts` | string | Yes | RFC 3339 timestamp (UTC) |

### Config Response

Sent automatically on connect and in response to a `get_config` query.

```json
{
  "type": "config",
  "namespace": "factory"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Always `"config"` |
| `namespace` | string | WarLink's configured namespace |

### Snapshot

Sent automatically on connect. Contains all current tag values across all PLCs. This is a single message, not one per tag.

```json
{
  "type": "snapshot",
  "tags": [
    {
      "plc": "MainPLC",
      "tag": "Counter",
      "alias": "Counter",
      "value": 42,
      "data_type": "DINT",
      "writable": true
    },
    {
      "plc": "MainPLC",
      "tag": "Temperature",
      "value": 72.5,
      "data_type": "REAL",
      "writable": false
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Always `"snapshot"` |
| `tags` | array | Array of tag objects. Each has the same fields as a tag change message (except `ts`) |

Each tag object in the array:

| Field | Type | Always Present | Description |
|-------|------|----------------|-------------|
| `plc` | string | Yes | PLC name |
| `tag` | string | Yes | Tag name |
| `alias` | string | No | Alias (if set) |
| `address` | string | No | Memory address (if applicable) |
| `value` | any | Yes | Current value |
| `data_type` | string | Yes | PLC data type |
| `writable` | bool | Yes | Write permission |

### Tag List Response

Sent in response to a `list_tags` query.

```json
{
  "type": "tag_list",
  "plcs": ["MainPLC", "SiemensPLC", "OmronPLC"],
  "tags": [
    {
      "plc": "MainPLC",
      "tag": "Counter",
      "value": 42,
      "data_type": "DINT",
      "writable": true
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Always `"tag_list"` |
| `plcs` | array | List of all configured PLC names |
| `tags` | array | All current tag values (same format as snapshot tags) |

### Pack List Response

Sent in response to a `list_packs` query.

```json
{
  "type": "pack_list",
  "packs": [
    {
      "name": "ProductionMetrics",
      "enabled": true,
      "members": 5
    },
    {
      "name": "AlarmStatus",
      "enabled": false,
      "members": 3
    }
  ]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Always `"pack_list"` |
| `packs` | array | Array of pack info objects |

Each pack object:

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | TagPack name |
| `enabled` | bool | Whether the pack is enabled |
| `members` | int | Number of member tags in the pack |

## Client-to-Server Requests

Clients send JSON requests on the same TCP connection. Each request is a single JSON object followed by `\n`.

### List Tags

Request a list of all PLCs and their current tag values.

```json
{"type": "list_tags"}
```

Response: [Tag List Response](#tag-list-response)

### List Packs

Request a list of all configured tagpacks.

```json
{"type": "list_packs"}
```

Response: [Pack List Response](#pack-list-response)

### Get Config

Request the current namespace configuration.

```json
{"type": "get_config"}
```

Response: [Config Response](#config-response)

### Replay

Request buffered events since a given timestamp. Events with timestamps strictly after the provided time are replayed in order.

```json
{"type": "replay", "since": "2024-01-15T10:00:00Z"}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Always `"replay"` |
| `since` | string | RFC 3339 timestamp. Events after this time are replayed. Accepts both second and nanosecond precision. |

The response is a stream of the original broadcast messages (tag, health, tagpack) replayed from the ring buffer. There is no envelope or "replay complete" marker — the events are sent directly to the client's stream and live events resume after.

If the timestamp is malformed, the request is silently ignored.

## Ring Buffer

The server maintains a fixed-size circular buffer of all broadcast events (tags, health, tagpacks). This enables clients to recover missed events after a disconnect.

- **Capacity**: Controlled by `buffer_size` (default 10,000 entries)
- **Eviction**: When full, the oldest entry is overwritten (FIFO)
- **Persistence**: The buffer is in-memory only; it does not survive WarLink restarts
- **Timestamps**: Each entry is stored with its broadcast timestamp for `replay` queries

### Sizing the Buffer

The appropriate `buffer_size` depends on your event volume:

| Scenario | Recommended Size |
|----------|-----------------|
| Low volume (< 10 tags, 1s poll) | 1,000 |
| Medium volume (100 tags, 500ms poll) | 10,000 |
| High volume (1000+ tags, fast poll) | 50,000 - 100,000 |

Each entry is the serialized JSON line plus a timestamp, typically 200-500 bytes. A buffer of 10,000 entries uses roughly 2-5 MB of memory.

## Client Behavior

### Per-Client Buffering

Each connected client has a send buffer (256 messages deep). Events are dispatched to all clients via non-blocking sends.

### Slow Client Handling

If a client's send buffer is full (the client is reading too slowly or the network is congested), events are **dropped silently** for that client. Other clients are unaffected.

This design prevents a single slow client from blocking event delivery to all clients or causing memory growth on the server. Dropped events can be recovered via the `replay` command after the client catches up.

### Write Timeout

TCP writes have a 5-second deadline. If a write to a client takes longer than 5 seconds, the client is disconnected.

### Connection Cleanup

When a client disconnects (either by closing the connection or due to a write error), its resources are cleaned up immediately. WarLink logs the disconnect.

## Integration Points

The warcry connector receives events from three sources within the WarLink engine:

| Source | Hook Location | Events |
|--------|--------------|--------|
| Value change handler | `engine/wiring.go` `setupValueChangeHandlers` | Tag changes from PLC polling |
| Health publisher | `engine/wiring.go` `publishAllHealth` | PLC health status (every 10s) |
| TagPack publish callback | `engine/engine.go` `SetOnPublish` | TagPack updates (debounced) |

All hooks check `HasClients()` before doing any serialization work, so the warcry connector has zero overhead when no clients are connected.

### Provider Interfaces

The connector is decoupled from WarLink's internal packages via two adapter interfaces defined in `warcry/server.go`:

```go
// PLCProvider supplies current tag values and PLC names.
type PLCProvider interface {
    GetAllCurrentValues() []TagSnapshot
    ListPLCNames() []string
}

// PackProvider supplies tagpack information.
type PackProvider interface {
    ListPacks() []PackInfo
}
```

These are implemented by adapter structs in `engine/wiring.go` (`warcryPLCAdapter`, `warcryPackAdapter`) that bridge to the PLC manager and tagpack manager.

### TagSnapshot

The warcry package defines its own tag value type to avoid importing `plcman`:

```go
type TagSnapshot struct {
    PLCName  string
    TagName  string
    Alias    string
    Address  string
    TypeName string
    Value    interface{}
    Writable bool
}
```

### PackInfo

```go
type PackInfo struct {
    Name    string
    Enabled bool
    Members int
}
```

## Testing

### Quick test with netcat

Start WarLink with warcry enabled, then connect with `nc`:

```bash
nc localhost 9999
```

You should immediately see a `config` line followed by a `snapshot` line. As PLC values change, `tag` events stream in. Health events appear every 10 seconds.

### Send queries

Type JSON into the netcat session (press Enter after each line):

```
{"type":"list_tags"}
{"type":"list_packs"}
{"type":"get_config"}
{"type":"replay","since":"2024-01-01T00:00:00Z"}
```

### Warcry client

Point a warcry instance at the same address:

```yaml
# warcry config.yaml
source:
  tcp:
    address: "127.0.0.1:9999"
    reconnect_delay: 5s
```

Warcry will:
1. Connect and receive the config response and snapshot
2. Send a replay request with its last-seen event timestamp (if any)
3. Send `list_tags`, `list_packs`, and `get_config` queries to populate its tag catalog
4. Process live events and evaluate alert rules against them

### Verify with jq

Pipe the TCP stream through `jq` for pretty-printed output:

```bash
nc localhost 9999 | jq .
```

Filter by message type:

```bash
nc localhost 9999 | while read -r line; do
  echo "$line" | jq -r 'select(.type == "tag")'
done
```

### Monitoring connected clients

When clients connect or disconnect, WarLink logs:

```
Warcry client connected: 192.168.1.50:54321 (id=0)
Warcry client disconnected: 192.168.1.50:54321 (id=0)
```

## Architecture

```
                         WarLink Engine
┌──────────────────────────────────────────────────────────────┐
│                                                              │
│  PLC Manager ──► Value Change Handler ──┐                    │
│                                         │                    │
│  Health Loop ──► publishAllHealth ──────┤                    │
│                                         ▼                    │
│  TagPack Mgr ──► SetOnPublish ───► Warcry Server            │
│                                    ┌─────────────┐          │
│                                    │ Ring Buffer  │          │
│                                    │ (10k events) │          │
│                                    └──────┬──────┘          │
│                                           │                  │
│                                    ┌──────┴──────┐          │
│                                    │  broadcast() │          │
│                                    └──────┬──────┘          │
│                                           │                  │
│                              ┌────────────┼────────────┐    │
│                              ▼            ▼            ▼    │
│                          client 0     client 1     client N │
│                          (ch 256)     (ch 256)     (ch 256) │
└──────────────────────────────────────────────────────────────┘
                              │            │            │
                              ▼            ▼            ▼
                         TCP socket   TCP socket   TCP socket
                              │            │            │
                              ▼            ▼            ▼
                          warcry        warcry       netcat
                         instance      instance      (debug)
```

### Threading Model

| Goroutine | Purpose |
|-----------|---------|
| Accept loop | Listens for new TCP connections |
| Per-client writer | Drains the client's send channel and writes to TCP |
| Per-client reader | Reads client requests (queries, replay) from TCP |
| Welcome sender | Sends config + snapshot on connect (fires once per client) |

Broadcast events are serialized in the caller's goroutine (the value change handler or health loop) and dispatched via non-blocking channel sends. No additional goroutine is created per broadcast.

### Resource Limits

| Resource | Limit | Configurable |
|----------|-------|--------------|
| Max concurrent clients | Unlimited | No |
| Per-client send buffer | 256 messages | No (compile-time) |
| Ring buffer capacity | 10,000 events | Yes (`buffer_size`) |
| TCP write timeout | 5 seconds | No (compile-time) |
| Scanner buffer | 64 KB | No (compile-time) |

## Differences from MQTT/Kafka/Valkey

The warcry connector is designed for real-time notification delivery, not data warehousing. Key differences from the other WarLink publishers:

| Feature | MQTT/Kafka/Valkey | Warcry |
|---------|-------------------|--------|
| Transport | MQTT/Kafka/Redis protocol | Raw TCP + NDJSON |
| Persistence | Broker-managed (retained, offsets) | In-memory ring buffer only |
| Backpressure | Broker-managed queues | Drop events for slow clients |
| Discovery | Topic patterns | Explicit `list_tags`/`list_packs` queries |
| Initial state | Retained messages (MQTT), key read (Valkey) | Snapshot on connect |
| Direction | Bidirectional (writeback support) | Read-only (server → client) |
| Filtering | Per-tag `no_mqtt`/`no_kafka`/`no_valkey` flags | All tags broadcast (no filtering) |
| TagPack payload | Same JSON | Wrapped in `{"type":"tagpack", ...}` envelope |

## Troubleshooting

### Server not starting

- Check that `enabled: true` and `listen` is set in the config
- Verify the port is not already in use: `lsof -i :9999`
- Check WarLink logs for "Warcry connector failed to start"

### Client connects but receives no events

- Verify at least one PLC is connected and has enabled tags
- Check that tag values are actually changing (the connector only broadcasts changes, not every poll)
- Health events should appear every 10 seconds regardless — if these are missing, the connector may not have started

### Missing events after reconnect

- Use the `replay` command with the timestamp of the last event you received
- If the disconnect was longer than the ring buffer covers, some events will be permanently lost — increase `buffer_size`
- The ring buffer does not survive WarLink restarts

### Client disconnecting unexpectedly

- Check for "Warcry client disconnected" in WarLink logs
- A 5-second write timeout may trigger if the client or network is too slow
- A full send buffer (256 messages) means the client is not reading fast enough
