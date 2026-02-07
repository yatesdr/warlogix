# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Changed
- **ADS Performance Optimization**: Implemented SumUp Read (IndexGroup 0xF080) for
  batching multiple tag reads into a single TCP request
  - ~98% reduction in read cycle time for multi-tag configurations
  - Example: 33 tags reduced from ~300ms to ~6ms per poll cycle
  - Uses direct addressing (IndexGroup 0x4040) for optimal compatibility

### Added
- **Debug Logging**: New `logging/debug.go` package for protocol-level troubleshooting
  - Hex dump capability for TX/RX packet inspection
  - Connection lifecycle logging
  - Works across all protocol drivers (ADS, S7, etc.)

## [0.1.8] - 2026-02-04

### Added
- **SSH Daemon Mode**: Run WarLogix as a background daemon with remote TUI access over SSH
  - Multiple SSH clients share the same TUI view via PTY multiplexing
  - Password and/or public key authentication support
  - Automatic host key generation (`~/.warlogix/host_key`)
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
