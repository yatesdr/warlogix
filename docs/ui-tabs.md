# User Interface Guide

WarLink uses a tabbed terminal interface (TUI) for managing PLCs, tags, and data brokers. Each tab displays its hotkey integrated into the name (e.g., **P**LCs, Repu**B**lisher, Tri**G**gers). Press `?` on any tab to see available keyboard shortcuts.

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

<img width="963" height="590" alt="image" src="https://github.com/user-attachments/assets/73d461a9-573d-4000-a4a0-348b79c69adb" />

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


### PLC Info

Displays the known information that was polled from the PLC.

<img width="389" height="274" alt="image" src="https://github.com/user-attachments/assets/86677928-ee77-4977-a485-328ed533e676" />

### Adding a PLC

<img width="385" height="259" alt="image" src="https://github.com/user-attachments/assets/a6e55cd7-4330-4290-9e4b-75d44071ff9a" />

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

Press `d` to discover Logix, S7, Omron, or Beckhoff PLCs on the local network. Discovery uses EtherNet/IP broadcasts as well as UDP and TCP discovery to find PLCs within about 10 seconds. Select a discovered device to pre-fill the Add dialog.   Discovery may be finicky depending on your network topology and broadcast domain, especially for UDP discovered PLCs.   This is a limitation of networking technology and all known best practices to find and add PLC's have been implemented.

<img width="772" height="375" alt="image" src="https://github.com/user-attachments/assets/69444e14-ad8e-462a-a638-41fee5be1f8a" />

---

## Republisher Tab

The Republisher tab displays available tags from connected PLCs. Use it to select which tags to publish and configure their properties.

<img width="963" height="590" alt="image" src="https://github.com/user-attachments/assets/484acd62-65b7-4cb0-9992-27a1a350d022" />

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
| `W` | **Write** value to selected tag (opens write dialog) |
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


<img width="318" height="208" alt="image" src="https://github.com/user-attachments/assets/2a5e6a29-251b-4375-be4d-647a12af13df" />

Press `s` on an enabled tag to configure which services receive that tag's updates:
- **REST API** - Available via HTTP endpoints
- **MQTT** - Published to MQTT brokers
- **Kafka** - Published to Kafka topics
- **Valkey** - Stored in Redis/Valkey keys

By default, all services are enabled. Disable services to reduce network traffic for specific tags if they are not needed.   Per-broker selection is not possible - it's all or none for a given service that's been configured.   A typical use case would be to publish tag states to Valkey or MQTT, and traceability or other defined packages of data to Kafka or MQTT QoS2.   For more granularity around specific tags and brokers you can run multiple warlink instances if they are properly namespaced and on separate IP Links.   You can't typically run multiple WarLink on the same IP address as the PLC's will often disconnect (ADS driver, in particular - protocol limited.)

### UDT/Structure Handling


<img width="390" height="183" alt="image" src="https://github.com/user-attachments/assets/edbc5d29-5709-4d14-8c15-89021fc3df97" />

When you select a UDT tag, press `Enter` to expand it and see its members. Each member can be independently enabled for publishing, or the entire UDT can be published, or some combination of both if desired.

**Ignore List:** Mark volatile UDT members (timestamps, counters, heartbeats) as "ignored" using `i`. Ignored members are still included in published data but don't trigger republishing when they change. This reduces message volume for frequently-changing status structures.

<img width="251" height="76" alt="image" src="https://github.com/user-attachments/assets/2eb29e17-87b5-4684-ba44-906cec02ad44" />

### Write Dialog

<img width="318" height="121" alt="image" src="https://github.com/user-attachments/assets/10203768-b2bc-48db-aedd-38fe86e4558f" />

Press `W` on any tag to write a new value. The dialog shows:
- Tag name and current data type
- Current value (read from PLC)
- Input field for new value

**Value Formats:**

| Data Type | Format | Example |
|-----------|--------|---------|
| BOOL | `true` or `false` | `true` |
| Integer (SINT, INT, DINT, LINT, etc.) | Decimal number | `42` |
| Float (REAL, LREAL) | Decimal number | `3.14159` |
| STRING | Plain text | `Hello World` |
| Arrays | Bracket notation | `[1 2 3 4 5]` |

**Array Syntax:**

- Use brackets with space or comma separation: `[1 2 3]` or `[1, 2, 3]`
- Boolean arrays: `[true false true]`
- String arrays: `[one two three]`
- Strings with spaces require quotes: `["hello world" "test string"]`

**Type-Aware Writes:** The write uses the tag's actual type from discovery, ensuring correct CIP type codes are sent. This prevents type mismatch errors that occur when the wrong data size is written.

---

## TagPacks Tab

TagPacks group tags from multiple PLCs and publish them atomically as a single JSON message when any non-ignored member changes.   You can think of them like a "Virtual" UDT, where multiple tags are grouped together and published under a new name.   This is very useful for dashboards that need data from several different tags, or even from several different PLCs.   TagPacks can pack tags from any configured PLC as long as the tag is enabled for publishing on that PLC.

**TagPacks are not guaranteed atomic reads** - This depends on the polling rates, especially across PLCs, as well as the PLC's specific implementation driver and whatever batch reading occurred to assemble the TagPack.   They should be considered to contain values that have occurred during the last polling cycle, but are not a deterministic item.   If you need atomic data synced in time, you will have to create a PLC function to provide it in a separate tag or UDT.

<img width="963" height="590" alt="image" src="https://github.com/user-attachments/assets/c9694947-08d5-41da-95f8-3f17c3c6cf2f" />


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

<img width="349" height="218" alt="image" src="https://github.com/user-attachments/assets/7baadf0f-fb01-4cf1-abf6-204454aade71" />

1. Tab to the Tag Packs pane and press `a` to create a new pack
2. Enter a name and topic
3. Select which brokers to publish to
4. Tab to the Member pane and press `a` to add tags from the tag picker

### Tag Picker

<img width="489" height="278" alt="image" src="https://github.com/user-attachments/assets/14bd732c-7df1-4913-8550-ebd3f20e38d1" />

When adding tags, use the filter to quickly find tags across all PLCs:

| Filter | Matches |
|--------|---------|
| `temp` | Any tag with "temp" in PLC name or tag name |
| `main:` | All tags from PLCs containing "main" |
| `:count` | Tags containing "count" from any PLC |
| `logix:prod` | Tags containing "prod" from PLCs containing "logix" |

---

## Triggers Tab

Event triggers capture data snapshots when PLC conditions are met and publish to MQTT and/or Kafka.  Event triggers are considered captured items and will not publish to Redis/Valkey services.  Triggers are typically used to capture all values at a particular moment, and most often interact with the PLC.   For example, if quality data is assembled into a bundle and then the PLC sets the "read_data" flag high, it can be monitored and then the Trigger pack will be assembled with the data.   Optional write-back is supported for PLC confirmation flags or status codes which need to be communicated back into the process.


<img width="963" height="590" alt="image" src="https://github.com/user-attachments/assets/36b2a192-94d2-4482-90f6-c5d50114210f" />


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

<img width="457" height="340" alt="image" src="https://github.com/user-attachments/assets/93f47185-c674-4527-9b8f-e3c13f8dfb75" />

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

<img width="963" height="590" alt="image" src="https://github.com/user-attachments/assets/9b8fa8a1-1500-4a89-9df5-0d5fe6022ef9" />


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

<img width="963" height="590" alt="image" src="https://github.com/user-attachments/assets/0d665e04-e800-4724-aefe-fa88a4ae8da9" />


### Display Columns

| Column | Description |
|--------|-------------|
| Status | Green if connected, gray if stopped |
| Name | Broker identifier |
| Broker | Hostname/IP |
| Port | TCP port |
| TLS | Yes/No |
| Selector | Sub-topic for all messages |
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
- **Selector** - Base topic prefix - optional.  Will be appended to the instance Namespace.
- **Client ID** - MQTT client identifier
- **Username/Password** - Optional authentication
- **Use TLS** - Enable encrypted connection
- **Auto-connect** - Connect on startup


### Publishing to Topics

Every WarLink instance requieres a Namespace at first launch.   It can be a city, factory, process line, or any other url-safe string, and will form the basis for publishing MQTT, Redis, and Kafka messages.   The 'Selector' will be appended to it for a per-server topic configuration.

Namespace:  warlink1
Selector: processData
MQTT Topic: /warlink1/processData/{messages}
Kafka Topic: warlink1-processData-{data}
Valkey Topic: warlink1:processData:{key}-->{data}

---

## Valkey Tab

The Valkey tab manages connections to Redis/Valkey servers.

<img width="963" height="590" alt="image" src="https://github.com/user-attachments/assets/4d1452ff-2048-43ec-9574-6295682196c9" />


### Display Columns

| Column | Description |
|--------|-------------|
| Status | Green if connected, gray if stopped |
| Name | Server identifier |
| Address | host:port |
| TLS | Yes/No |
| Selector | Sub-namespace for key prefixes |
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
- **Selector** - Sub-namespace appended to the global namespace for key prefixes
- **Key TTL** - Key expiration in seconds (0 = no expiry)
- **Use TLS** - Enable encrypted connection
- **Publish Changes** - Enable Pub/Sub notifications
- **Enable Writeback** - Enable write-back queue for PLC writes
- **Auto-connect** - Connect on startup

### Publishing to Topics

Every WarLink instance requieres a Namespace at first launch.   It can be a city, factory, process line, or any other url-safe string, and will form the basis for publishing MQTT, Redis, and Kafka messages.   The 'Selector' will be appended to it for a per-server topic configuration.

Namespace:  warlink1
Selector: processData
MQTT Topic: /warlink1/processData/{messages}
Kafka Topic: warlink1-processData-{data}
Valkey Topic: warlink1:processData:{key}-->{data}
---

## Kafka Tab

The Kafka tab manages connections to Kafka clusters.

<img width="963" height="590" alt="image" src="https://github.com/user-attachments/assets/9abb9225-3693-4183-971e-1ec22f9e1688" />


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

<img width="963" height="590" alt="image" src="https://github.com/user-attachments/assets/9b0653b4-fcf3-4b7a-ae18-fb06cdb2dcaf" />


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

To log debug messages to a file, start WarLink with:

```bash
./warlink --log /path/to/logfile.log
```

For verbose protocol debugging with hex dumps:

```bash
./warlink --log-debug
```

This creates `debug.log` with detailed protocol-level information.
