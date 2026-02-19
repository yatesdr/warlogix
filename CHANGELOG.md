# Changelog

All notable changes to this project will be documented in this file.

## [0.2.15] - 2026-02-18

### Added
- **REST API SSE Stream**: New `GET /api/events` endpoint streams real-time PLC
  data as Server-Sent Events. Event types: `value-change` (tag value changes),
  `tagpack` (TagPack publishes), `status-change` (PLC connect/disconnect), and
  `health` (periodic health checks). Supports `?types=` and `?plc=` query
  parameter filters. No authentication required, consistent with the REST API
  design for SCADA integration.

## [0.2.14] - 2026-02-18

### Changed
- **PCCC Batch Reads**: Contiguous full-element reads in the same data file are
  now batched into single PCCC round-trips automatically (via plcio v0.1.5).
  A PLC with 50 integer tags in N7 now reads them in one command instead of
  fifty. Sub-element and bit reads remain individual.

## [0.2.13] - 2026-02-18

### Added
- **PCCC Data Table Discovery**: SLC 500 and MicroLogix PLCs now support automatic
  data table discovery via the file directory (system file 0). The "Discover Tags"
  checkbox is available in both the TUI and Web UI for these families. PLC-5 remains
  manual-only (no file directory support).

### Fixed
- **WebUI PLC Edit Reconnect**: Editing a PLC's family or address in the Web UI now
  triggers a reconnect so the new driver takes effect immediately. Previously, changes
  were saved to config but the runtime connection was not refreshed.
- **WebUI Tag DataType Edit**: Editing a tag's DataType in the Web UI now refreshes
  the runtime tag list immediately. Previously, changes were saved but the tag browser
  continued using the stale type until restart.

## [0.2.12] - 2026-02-18

### Added
- **PCCC Family Support (Experimental)**: Added support for Allen-Bradley legacy PLC
  families using PCCC-over-EtherNet/IP: SLC-500, PLC/5, and MicroLogix. These appear
  as separate families (`slc500`, `plc5`, `micrologix`) in the TUI, Web UI, and config.
  Tags use data table addressing (e.g., `N7:0`, `F8:5`, `T4:0.ACC`). Optional connection
  path field for CIP gateway routing.
  **This feature is untested against real hardware and should be considered experimental.**
- **Connection Path Documentation**: Added documentation for the Logix CIP connection
  path field with routing examples.

## [0.2.11] - 2026-02-18

### Added
- **Connection Path Field**: Added connection path field for Logix PLCs in the Web UI,
  TUI, and REST API for multi-hop CIP routing through communication modules.

### Fixed
- **PLCs Tab Tag Count**: Fixed PLCs tab showing 0 tags for auto-discovered PLCs.

### Changed
- **Tag Tree Performance**: Optimized tag tree loading with lazy-load JSON values and
  cached config maps for faster rendering with large tag lists.

## [0.2.10] - 2026-02-18

### Added
- **PCCC Family Support (Experimental)**: Added support for Allen-Bradley legacy PLC
  families using PCCC-over-EtherNet/IP: SLC-500, PLC/5, and MicroLogix. These appear
  as separate families (`slc500`, `plc5`, `micrologix`) in the TUI, Web UI, and config.
  Tags use data table addressing (e.g., `N7:0`, `F8:5`, `T4:0.ACC`). No tag discovery
  (address-based only). Optional connection path field for CIP gateway routing.
  **This feature is untested against real hardware and should be considered experimental.**

## [0.2.9] - 2026-02-16

### Added
- **TUI Condition CRUD**: Add, edit, and delete rule conditions directly from the
  conditions table (`a`/`e`/`x` keys). Status bar shows available keys when focused.
- **Web UI Light Mode**: Proper `color-scheme` support so native form controls
  (checkboxes, selects, scrollbars) respect the active theme. Added `accent-color`
  theming for checkboxes and `.checkbox-label` styles.

### Changed
- **TUI Edit Dialog**: Simplified to rule-level settings only (logic, debounce,
  cooldown). Condition management moved to the conditions table.
- **Web UI Modals**: Use a subtly raised surface color with border for better
  contrast against the page background in both light and dark modes.
- **Web UI Forms**: Explicit `background` and `color` on all form inputs and
  selects prevents OS dark-mode bleed-through in light mode.
- **Debug Log Timestamps**: Consistent `YYYY-MM-DD HH:MM:SS` format for both
  initial page load and live SSE updates.

### Fixed
- **Rule Error Log Spam**: Deduplicate repeated evaluation errors so the same
  error (e.g. type mismatch) is logged once instead of every 100ms.
- **Web UI Dark Mode**: Replaced hardcoded hex colors in dark-mode form overrides
  with CSS variables for consistency.

## [0.2.8] - 2026-02-15

### Added
- **Rules Engine**: Unified automation system replacing separate Triggers and Push systems. Rules support multiple conditions (AND/OR logic), multiple action types per rule, and both rising-edge and falling-edge (cleared) actions.
  - **Publish actions**: Capture tags or TagPacks and publish to MQTT (QoS 2) and/or Kafka
  - **Webhook actions**: Send HTTP requests to external endpoints with body templates and authentication
  - **Writeback actions**: Write values to PLC tags on fire or clear
- **Engine Package**: Extracted `engine/` package that orchestrates all managers (PLC, MQTT, Valkey, Kafka, Rules, TagPacks, Warcry) with a clean request/response API
- **Warcry Server**: New TCP streaming server for real-time PLC event notifications with ring buffer replay
- **Rule Manager Web UI**: Create, edit, and manage rules from the browser with a form-based editor

### Changed
- **PLC Drivers Refactored**: All PLC driver code (Logix, S7, ADS, FINS, Omron EIP) extracted to separate [`plcio`](https://github.com/yatesdr/plcio) module for reuse in other projects
- **Config Simplification**: Removed legacy `triggers`, `pushes`, and `rest` config sections; replaced with unified `rules` section
- **Web UI Navigation**: Improved sidebar navigation and renamed Events page to Rule Manager
- **TUI Tab**: Triggers and Push tabs replaced with unified Rules tab (`R` hotkey)

### Removed
- **Trigger Package**: Replaced by Rules engine (`rule/` package)
- **Push Package**: Replaced by Rules engine webhook actions
- **Standalone PLC Drivers**: Moved to `plcio` module (ads, cip, eip, logix, omron, s7, driver packages)
- **Logging Package**: Removed in favor of standard library logging
- **Brokertest Package**: Removed unused test helper
- Dead code identified by static analysis

## [0.2.7] - 2026-02-13

### Fixed
- **Omron Write Type Conversion**: Fix "cannot convert float64 to DINT" error when writing to Omron PLCs via web UI or REST API. Added float64 handling for all integer types (BOOL, BYTE, SINT, WORD, INT, DWORD, DINT, LWORD, LINT) in both EIP and FINS write paths.
- **TUI Stale Type Display**: Fix tag types showing stale values (e.g., INT instead of DINT) after the poll loop corrects them from CIP response. The inline type update now triggers a tree rebuild so corrected types are immediately visible.

## [0.2.6] - 2026-02-13

### Added
- **Push Manager**: New `push` package for tag push functionality with configurable push targets, scheduling, and tests
- **Push Tab**: New TUI tab for managing push configurations
- **Setup Wizard**: First-run setup page and namespace configuration in the web UI
- **Change Password Page**: Web UI page for user password changes
- **Omron Reference Code**: C++ reference implementations for future Omron NJ/NX driver work
- **Web Server Tests**: Added test coverage for the web server and login flow
- **Dark Mode**: Web UI dark mode support with theme toggle
- **Republisher PLC Picker**: Select PLCs directly from the republisher tree in the web UI

### Changed
- **CLI Flag Renames**: `-p` is now HTTP port (was SSH port), `--ssh-port` for SSH, `--ssh-pass` (was `--ssh-password`), `--admin-user`/`--admin-pass` (was `--web-admin-user`/`--web-admin-pass`), `--host` (was `--web-host`), `--no-api`/`--no-webui` for selective disabling
- **Web Server Consolidation**: Replaced standalone API server with consolidated web server, removing `api/server.go` in favor of unified `web/server.go` + `www/` handlers
- **Expanded Web API**: Significantly expanded REST API handlers for PLC management, tag operations, and service control
- **SSE Enhancements**: Additional server-sent event types for real-time web UI updates
- **TUI Refactor**: TUI now uses a `WebServer` interface to avoid import cycles with the web package
- **Config Expansion**: Extended configuration structs for push targets and web server settings
- **Omron Discovery Improvements**: Major expansion of Omron EIP discovery and connected messaging
- **PLC Manager**: Extended manager with push integration and expanded PLC lifecycle management

### Fixed
- **Omron NX1P2 EIP/CIP**: Fixed discovery, connected messaging, and reconnection for NX1P2 controllers
- **Web UI Styling**: CSS refinements and layout fixes across republisher, PLC, and REST pages

## [0.2.4] - 2026-02-09

### Added
- **Documentation**: Comprehensive guides for API examples, best practices, migration, troubleshooting, and use cases

### Fixed
- **SSH Disconnect**: Shift-Q no longer requires double press to disconnect
- **Terminal Restore**: Terminal properly restores after SSH disconnect without requiring keypress
- **Daemon Mode Stability**: PLC tab now refreshes correctly after SSH reconnection
- **SSH Protocol**: Added proper exit-status signaling for clean session termination

### Changed
- Cleaned up unused SSH code (AuthConfig, Session.Read/Write, window channels)

## [0.2.3] - 2026-02-09

### Changed
- **Independent SSH Sessions**: Each SSH connection now gets its own independent TUI instance
  - Multiple users can navigate different tabs and work independently
  - Replaces previous shared-screen PTY multiplexing architecture
- **Real-Time Config Sync**: Configuration changes sync across all connected sessions
  - Tag enable/disable and writable state syncs instantly
  - REST, MQTT, Valkey, Kafka service state syncs across sessions
  - Trigger enable/disable syncs across sessions
  - Cursor position preserved when syncing browser tab state
- **Trigger Cooldown Indicator**: Changed from blue to orange for better visual clarity
- **MQTT Tab**: Renamed "Root Topic" column to "Selector" for consistency
- **Kafka Tab**: Selector now always shown in Cluster Info pane (displays "(none)" when empty)
- **REST Tab**: Host and Port fields now sync across sessions

### Removed
- PTY multiplexing dependency (creack/pty) - no longer required

### Fixed
- Blank screen on SSH connect due to double screen initialization
- REST tab input fields not syncing across sessions

## [0.2.0] - 2026-02-09

### Added
- **Siemens S7 Write Support**: Full write capability for S7-300/400/1200/1500 PLCs
  - Chunked writes for large data blocks
  - Support for all basic types (BOOL, INT, DINT, REAL, STRING, etc.)
  - Array write support
- **Native S7 Protocol Implementation**: Replaced gos7 dependency with custom implementation
  - Better error handling and connection recovery
  - Improved compatibility across S7 variants
- **Multi-Protocol PLC Discovery**: Scan network for PLCs from all supported vendors
  - EtherNet/IP broadcast for Allen-Bradley and Omron NJ/NX
  - S7 identification for Siemens PLCs
  - ADS/UDP broadcast for Beckhoff TwinCAT
  - FINS/UDP for Omron CS/CJ/CP series
  - Live discovery modal with real-time results
- **Write Dialog**: New TUI dialog for writing values to PLC tags
  - Type-aware writes using discovered tag types
  - Array support with bracket notation
  - String and quoted string array parsing
  - Support for BYTE/WORD/DWORD/LWORD types
- **TagPacks**: Group tags from multiple PLCs for atomic publishing
  - Publish as single JSON message when any member changes
  - Per-pack broker selection (MQTT, Kafka, Valkey)
  - Ignore list for volatile members (timestamps, counters)
- **Theming System**: 12 built-in color themes
  - Cycle with F6 key
  - Persistent theme selection in config
- **Unified Namespace**: Consistent topic/key structure across all brokers
  - Configurable namespace prefix
  - Per-broker selector suffix
- **Health Monitoring**: Periodic health status publishing
  - Per-PLC toggle for health checks
  - Published to REST, MQTT, Valkey, and Kafka
- **UDT/Structure Support**: Automatic structure unpacking
  - Template discovery for Logix and Beckhoff
  - Member-level change filtering with ignore list
  - Published as JSON objects
- **Stress Testing**: Built-in broker performance testing
  - `--stress-test-republishing` flag
  - Configurable duration, tag count, and PLC count
  - Measures confirmed delivery throughput
- **Comprehensive Documentation**
  - Daemon mode setup guide with systemd and OpenRC examples
  - User interface guide with all keyboard shortcuts
  - Developer guide for using drivers in custom applications
  - Performance tuning guide
  - Multi-instance deployment guide
  - Safety and intended use guidelines

### Changed
- **ADS Performance Optimization**: SumUp Read batching reduces read cycle time by ~98%
- **Omron Improvements**: Optimized FINS and EIP drivers for high-throughput reads
- **Kafka Publishing**: Batched async publishing with confirmation tracking
- **MQTT Publishing**: Uses tag alias for topic path when available
- **REST Tab**: Initial focus set to Start button to keep hotkeys active
- **Debug Logging**: Protocol-level hex dumps available via `--log-debug`

### Fixed
- TUI filter and hotkey handling bugs
- Logix write type detection for UDT members
- ADS type encoding for writes
- STRING type decoding (proper null termination)
- FINS address parsing for multi-digit addresses (DM10, DM100, etc.)
- FINS/TCP node address negotiation
- Discovery dialog race conditions with other modals
- Write dialog deadlocks and rendering issues
- ASCII mode detection from locale environment
- Race conditions and goroutine leaks in various components

## [0.1.8] - 2026-02-04

### Added
- **SSH Daemon Mode**: Run WarLink as a background daemon with remote TUI access over SSH
  - Multiple SSH clients share the same TUI view via PTY multiplexing
  - Password and/or public key authentication support
  - Automatic host key generation (`~/.warlink/host_key`)
  - Services continue running even with no SSH connections
  - Graceful shutdown with SIGTERM/SIGINT signals
- **File Logging**: New `--log` flag to write debug messages to a file
  - Works in both local and daemon modes
  - Logs written alongside the debug window (not instead of)
  - Color tags stripped for clean log file output
- New CLI flags: `-d`, `-p`, `--ssh-password`, `--ssh-keys`, `--log`

### Changed
- `Shift+Q` now disconnects from daemon (vs quit in local mode)
- Help text updated to reflect daemon mode differences
- Updated documentation with daemon mode usage, configuration, and shutdown procedures

### Dependencies
- Added `github.com/gliderlabs/ssh` for SSH server
- Added `github.com/creack/pty` for PTY handling
- Added `golang.org/x/crypto` for SSH key management

## [0.1.7] - 2026-02-04

### Added
- **Omron FINS Protocol Support**: New `fins` package for Omron CJ/CS/CP/NJ/NX series PLCs via FINS/UDP
  - Memory area access (DM, CIO, WR, HR, AR)
  - Word and bit-level read/write operations
  - Big-endian byte order (native Omron format)
  - Type hints for proper data interpretation
- **Beckhoff TwinCAT Support**: New `ads` package for TwinCAT 2/3 PLCs via ADS protocol
  - Automatic symbol discovery
  - AMS Net ID routing
  - Little-endian byte order (native TwinCAT format)
- **Kafka Integration**: Publish tag values and event triggers to Apache Kafka
  - Topic-per-PLC publishing
  - Event trigger snapshots with acknowledgment
  - Configurable producer settings
- **Siemens S7 Improvements**
  - String array support
  - Connection recovery and watchdog monitoring
  - Improved error handling
- **Allen-Bradley Logix Improvements**
  - Large array support (automatic chunking)
  - Structure/UDT support
  - Full Micro800 array support with automatic tag discovery
- **Event Triggers**: Capture data snapshots on PLC events and publish to Kafka
- **Per-PLC Poll Rate**: Configure different polling intervals for each PLC

### Changed
- Updated documentation with comprehensive PLC configuration guides
- Improved byte order documentation for known vs unknown data types
- Enhanced write request documentation for MQTT, Valkey, and Kafka

### Fixed
- Type assertion bugs in value handling
- S7 addressing format documentation (now uses `DB<n>.<offset>` style)
- Port exhaustion issues with connection management

## [0.1.6] - Previous Release

Initial public release with Allen-Bradley Logix and Siemens S7 support.
