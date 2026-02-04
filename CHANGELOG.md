# Changelog

All notable changes to this project will be documented in this file.

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
