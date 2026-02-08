# User Interface Guide

WarLogix uses a tabbed terminal interface (TUI) for managing PLCs, tags, and data brokers. Each tab displays its hotkey integrated into the name (e.g., **P**LCs, Repu**B**lisher, Tri**G**gers). Press `?` on any tab to see available keyboard shortcuts.

## Global Shortcuts

| Key | Action |
|-----|--------|
| `P` | Jump to **P**LCs tab |
| `B` | Jump to Repu**B**lisher tab |
| `T` | Jump to **T**agPacks tab |
| `G` | Jump to Tri**G**gers tab |
| `E` | Jump to R**E**ST tab |
| `M` | Jump to **M**QTT tab |
| `V` | Jump to **V**alkey tab |
| `K` | Jump to **K**afka tab |
| `D` | Jump to **D**ebug tab |
| `Shift+Tab` | Cycle through tabs |
| `N` | Configure namespace |
| `F6` | Cycle color themes |
| `?` | Show help dialog |
| `Q` | Quit application |

---

## PLCs Tab

The PLCs tab manages PLC connections. It lists all configured PLCs with their connection status, family type, and device information.

### Display Columns

| Column | Description |
|--------|-------------|
| Status | Connection indicator (green=connected, yellow=connecting, red=error, gray=disconnected) |
| Name | PLC identifier (unique name) |
| Address | IP address or hostname |
| Family | PLC type (logix, micro800, s7, beckhoff, omron) |
| Status | Connection state text |
| Product | Device model name (shown when connected) |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `d` | **Discover** - Scan network for Allen-Bradley PLCs |
| `a` | **Add** - Add a new PLC manually |
| `e` | **Edit** - Modify selected PLC configuration |
| `x` | **Remove** - Delete selected PLC (with confirmation) |
| `c` | **Connect** - Connect to selected PLC |
| `C` | **Disconnect** - Disconnect from selected PLC |
| `i` | **Info** - Show detailed device information |
| `Enter` | Toggle connection (connect if disconnected, disconnect if connected) |

### Adding a PLC

Press `a` to open the Add PLC dialog:

1. **Family** - Select the PLC type:
   - `logix` - Allen-Bradley ControlLogix/CompactLogix
   - `micro800` - Allen-Bradley Micro800 series
   - `s7` - Siemens S7-300/400/1200/1500
   - `beckhoff` - Beckhoff TwinCAT
   - `omron` - Omron CS/CJ/NJ/NX series

2. **Name** - Unique identifier for this PLC

3. **Address** - IP address or hostname

4. **Slot** - CPU slot number (varies by family)
   - Logix/Micro800: Typically 0
   - S7-300/400: Usually 2
   - S7-1200/1500: Always 0

5. **Poll Rate (ms)** - How often to read tags (250-10000ms)

6. **Auto-connect** - Automatically connect on startup

7. **Health check** - Publish health status periodically

For Beckhoff, also configure:
- **AMS Net ID** - The PLC's AMS address (e.g., 192.168.1.100.1.1)
- **AMS Port** - 851 for TwinCAT 3, 801 for TwinCAT 2

For Omron, also configure:
- **Protocol** - `fins` for CS/CJ/CP series, `eip` for NJ/NX series

### Discovery

Press `d` to discover Allen-Bradley PLCs on the local network. Discovery uses EtherNet/IP broadcast to find PLCs within 3 seconds. Select a discovered device to pre-fill the Add dialog.

---

## Browser Tab

The Browser tab displays available tags from connected PLCs. Use it to select which tags to publish and configure their properties.

### Display Elements

- **PLC dropdown** - Select which PLC to browse
- **Filter input** - Filter tags by name
- **Tag tree** - Hierarchical view of programs and tags
- **Details panel** - Shows selected tag information and value

### Tag Tree Structure

For discovery-based PLCs (Logix, Beckhoff, Omron EIP):
- **Controller** - Controller-scoped tags
- **[Program Name]** - Program-scoped tags

For manual PLCs (S7, Omron FINS):
- **Manual Tags** - Configured tags

### Tag Indicators

| Indicator | Meaning |
|-----------|---------|
| `[x]` | Tag enabled for publishing |
| `[ ]` | Tag not publishing |
| `W` | Tag is writable |
| `I` | Tag changes ignored (for UDT members) |
| `!` | Tag read error |
| `▶` | UDT/Structure (expandable) |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Space` | Toggle tag publishing (enable/disable) |
| `w` | Toggle writable flag |
| `i` | Toggle ignore for change detection (UDT members) |
| `s` | Configure per-service publishing (REST/MQTT/Kafka/Valkey) |
| `d` | Show detailed tag information with live value and hex dump |
| `/` | Focus filter input |
| `c` | Clear filter |
| `p` | Focus PLC dropdown |
| `Tab` | Switch focus to details panel |
| `Enter` | Expand/collapse UDT |

For manual PLCs (S7, Omron FINS):

| Key | Action |
|-----|--------|
| `a` | Add manual tag |
| `e` | Edit selected tag |
| `x` | Delete selected tag |

### Per-Service Publishing

Press `s` on an enabled tag to configure which services receive that tag's updates:
- **REST API** - Available via HTTP endpoints
- **MQTT** - Published to MQTT brokers
- **Kafka** - Published to Kafka topics
- **Valkey** - Stored in Redis/Valkey keys

By default, all services are enabled. Disable services to reduce network traffic for specific tags.

### UDT/Structure Handling

When you select a UDT tag, press `Enter` to expand it and see its members. Each member can be independently enabled for publishing.

**Ignore List:** Mark volatile UDT members (timestamps, counters, heartbeats) as "ignored" using `i`. Ignored members are still included in published data but don't trigger republishing when they change. This reduces message volume for frequently-changing status structures.

---

## Packs Tab

TagPacks group tags from multiple PLCs and publish them atomically as a single JSON message when any non-ignored member changes.

### Display Columns

| Column | Description |
|--------|-------------|
| Status | Green if enabled, gray if disabled |
| Name | Pack identifier |
| Members | Number of tags in the pack |
| Topic | Publishing topic/channel |
| MQTT/Kafka/Valkey | Enabled services (● enabled, ○ disabled) |

### Keyboard Shortcuts

The TagPacks tab uses context-sensitive hotkeys based on which pane has focus:

**Pack list (left pane):**

| Key | Action |
|-----|--------|
| `a` | **Add** new pack |
| `x` | **Remove** selected pack (with confirmation) |
| `Space` | Toggle pack enabled/disabled |
| `e` | **Edit** pack settings |
| `Tab` | Switch focus to member list |

**Member list (right pane):**

| Key | Action |
|-----|--------|
| `a` | **Add** tag to pack |
| `x` | **Remove** selected member (with confirmation) |
| `i` | Toggle **ignore** (ignored members don't trigger publish) |
| `E` | **Enable** tag in Browser (if not already enabled) |

### Creating a Pack

1. Press `c` to create a new pack
2. Enter a name and topic
3. Select which brokers to publish to
4. Press `a` to add tags from the tag picker

### Tag Picker

When adding tags, use the filter to quickly find tags across all PLCs:

| Filter | Matches |
|--------|---------|
| `temp` | Any tag with "temp" in PLC name or tag name |
| `main:` | All tags from PLCs containing "main" |
| `:count` | Tags containing "count" from any PLC |
| `logix:prod` | Tags containing "prod" from PLCs containing "logix" |

---

## Triggers Tab

Event triggers capture data snapshots when PLC conditions are met and publish to MQTT and/or Kafka.

### Display Columns

| Column | Description |
|--------|-------------|
| Status | Armed (green), Firing (yellow), Cooldown (blue), Error (red), Stopped (gray) |
| Name | Trigger identifier |
| PLC | Source PLC |
| Trigger | Tag being monitored |
| Condition | Comparison operator and value |
| Pack | Optional TagPack to publish on fire |
| Fires | Total fire count since startup |
| Status | Current state text |

### Keyboard Shortcuts

The Triggers tab uses context-sensitive hotkeys based on which pane has focus:

**Trigger list (left pane):**

| Key | Action |
|-----|--------|
| `a` | **Add** new trigger |
| `x` | **Remove** selected trigger (with confirmation) |
| `e` | **Edit** selected trigger |
| `s` | **Start** (arm) trigger |
| `S` | **Stop** (disarm) trigger |
| `T` | **Test** fire trigger manually (does not enter cooldown) |
| `Tab` | Switch focus to data tags list |

**Data tags list (right pane):**

| Key | Action |
|-----|--------|
| `a` | **Add** tag or pack to capture list |
| `x` | **Remove** selected tag (with confirmation) |

### Creating a Trigger

1. Press `a` to open the Add dialog
2. Configure:
   - **Name** - Unique identifier
   - **PLC** - Source PLC
   - **Trigger Tag** - Tag to monitor (from published tags)
   - **Operator** - Comparison (==, !=, >, <, >=, <=)
   - **Value** - Threshold value
   - **Ack Tag** - Optional tag to write acknowledgment (1=success, -1=error)
   - **MQTT Broker** - Select "All", "None", or a specific broker (publishes with QoS 2)
   - **Kafka Cluster** - Select "All", "None", or a specific cluster
   - **Publish Pack** - Optional TagPack to publish on fire
3. Use `a` in the data tags pane to add tags or packs to capture when triggered

### Test Firing

Press `T` to manually test fire a trigger. This:
- Bypasses the condition check
- Captures configured data tags
- Publishes to configured MQTT brokers and Kafka clusters
- Does **not** enter cooldown state (trigger remains armed)
- Works even when the trigger is disabled

---

## REST Tab

The REST tab configures the HTTP API server.

### Configuration

- **Host** - Bind address (0.0.0.0 for all interfaces)
- **Port** - HTTP port (default: 8080)
- **Start/Stop** - Control server state

### Navigation

Use `Tab` to move between fields and buttons:
1. Host input
2. Port input
3. Start button
4. Stop button

### Endpoints

The tab displays all available REST endpoints with their HTTP methods and URL patterns. See [REST API Reference](rest-api.md) for complete documentation.

---

## MQTT Tab

The MQTT tab manages connections to MQTT brokers.

### Display Columns

| Column | Description |
|--------|-------------|
| Status | Green if connected, gray if stopped |
| Name | Broker identifier |
| Broker | Hostname/IP |
| Port | TCP port |
| TLS | Yes/No |
| Root Topic | Base topic for all messages |
| Status | Connection state |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `a` | **Add** new broker |
| `e` | **Edit** selected broker |
| `x` | **Remove** selected broker (with confirmation) |
| `c` | **Connect** to selected broker |
| `C` | **Disconnect** from selected broker |
| `Enter` | Toggle connection |

### Broker Configuration

- **Name** - Unique identifier
- **Broker** - Hostname or IP address
- **Port** - MQTT port (default: 1883, TLS: 8883)
- **Root Topic** - Base topic prefix
- **Client ID** - MQTT client identifier
- **Username/Password** - Optional authentication
- **Use TLS** - Enable encrypted connection
- **Auto-connect** - Connect on startup

---

## Valkey Tab

The Valkey tab manages connections to Redis/Valkey servers.

### Display Columns

| Column | Description |
|--------|-------------|
| Status | Green if connected, gray if stopped |
| Name | Server identifier |
| Address | host:port |
| TLS | Yes/No |
| Factory | Key prefix |
| Status | Connection state |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `a` | **Add** new server |
| `e` | **Edit** selected server |
| `x` | **Remove** selected server (with confirmation) |
| `c` | **Connect** to selected server |
| `C` | **Disconnect** from selected server |
| `Enter` | Toggle connection |

### Server Configuration

- **Name** - Unique identifier
- **Address** - host:port (default: localhost:6379)
- **Password** - Optional authentication
- **Database** - Redis database number (0-15)
- **Factory** - Key prefix for all keys
- **Key TTL** - Key expiration in seconds (0 = no expiry)
- **Use TLS** - Enable encrypted connection
- **Publish Changes** - Enable Pub/Sub notifications
- **Enable Writeback** - Enable write-back queue for PLC writes
- **Auto-connect** - Connect on startup

---

## Kafka Tab

The Kafka tab manages connections to Kafka clusters.

### Display Columns

| Column | Description |
|--------|-------------|
| Status | Green if connected, gray if stopped, yellow if connecting, red if error |
| Name | Cluster identifier |
| Brokers | Bootstrap broker addresses |
| TLS | Yes/No |
| SASL | Authentication mechanism or "-" |
| Status | Connection state |

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `a` | **Add** new cluster |
| `e` | **Edit** selected cluster |
| `x` | **Remove** selected cluster (with confirmation) |
| `c` | **Connect** to selected cluster |
| `C` | **Disconnect** from selected cluster |

### Cluster Configuration

- **Name** - Unique identifier
- **Brokers** - Comma-separated broker addresses
- **Use TLS** - Enable encrypted connection
- **SASL** - Authentication: None, PLAIN, SCRAM-SHA-256, SCRAM-SHA-512
- **Username/Password** - SASL credentials
- **Publish Changes** - Enable tag change publishing
- **Topic** - Topic for tag changes
- **Auto-create Topics** - Let broker auto-create topics
- **Auto-connect** - Connect on startup

---

## Debug Tab

The Debug tab displays real-time log messages for troubleshooting.

### Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `c` or `C` | **Clear** the log |
| `g` | Go to **top** (beginning) |
| `G` | Go to **bottom** (end) |
| `↑/↓` | Scroll through log |

### Log Message Types

Messages are color-coded by category:
- **INFO** - General informational messages
- **ERROR** - Error conditions (red)
- **MQTT** - MQTT connection and publish events (green)
- **VALKEY** - Redis/Valkey operations (accent color)
- **KAFKA** - Kafka producer events
- **SSH** - SSH daemon events
- **Logix** - Allen-Bradley protocol messages

### File Logging

To log debug messages to a file, start WarLogix with:

```bash
./warlogix --log /path/to/logfile.log
```

For verbose protocol debugging with hex dumps:

```bash
./warlogix --log-debug
```

This creates `debug.log` with detailed protocol-level information.
