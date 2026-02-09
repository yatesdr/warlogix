# Changelog

All notable changes to this project will be documented in this file.

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
