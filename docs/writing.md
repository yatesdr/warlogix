# Writing Values to PLCs

This document describes how to write values to PLC tags using WarLogix. **Writing is designed for occasional status updates and acknowledgments, not high-frequency control.**

## Intended Use

Writing values in WarLogix is primarily intended for:

- **Status code write-back** - Confirming trigger execution success/failure
- **Acknowledgment flags** - Signaling that data was captured or processed
- **Simple parameter updates** - Occasional configuration changes via dashboard

Writing is **not** intended or optimized for:

- High-frequency writes
- Real-time control loops
- Motion control or safety functions
- Bulk data transfer

For a complete discussion of proper write-back usage, see [Safety and Intended Use](safety-and-intended-use.md).

---

## Using the TUI Write Feature

### Enabling Writable Tags

Before writing to a tag, it must be marked as writable:

1. Navigate to the tag in the Browser tab
2. Press `w` to toggle writable status (a "W" indicator appears)
3. The tag is now enabled for writes

### Writing a Value

1. Select the writable tag in the Browser
2. Press `W` (Shift+W) to open the write dialog
3. Enter the new value in the input field
4. Press Enter or click "Write" to send the value
5. Press Escape to cancel

### Value Input Format

| Type | Input Examples |
|------|----------------|
| Integer types | `42`, `-17`, `0` |
| Floating point | `3.14`, `-0.5`, `1.0e6` |
| Boolean | `true`, `false`, `1`, `0` |
| Arrays | `[1, 2, 3]` or `1 2 3` |
| Strings | `hello` or `"hello world"` |

---

## Supported Data Types by PLC Family

### Allen-Bradley Logix (ControlLogix, CompactLogix, Micro800)

| Type | Write Support | Notes |
|------|---------------|-------|
| BOOL | Yes | |
| SINT | Yes | 8-bit signed |
| INT | Yes | 16-bit signed |
| DINT | Yes | **Recommended** - most reliable |
| LINT | Yes | 64-bit signed |
| REAL | Yes | 32-bit float |
| LREAL | Yes | 64-bit float |
| STRING | Yes | |
| Arrays | Yes | All scalar types |
| UDT members | Yes | Access via `Tag.Member` |

### Siemens S7 (S7-300, S7-400, S7-1200, S7-1500)

| Type | Write Support | Notes |
|------|---------------|-------|
| BOOL | Yes | Bit writes (defaults to bit 0) |
| BYTE/USINT | Yes | 8-bit unsigned |
| SINT | Yes | 8-bit signed |
| WORD/UINT | Yes | 16-bit unsigned |
| INT | Yes | 16-bit signed |
| DWORD/UDINT | Yes | 32-bit unsigned |
| DINT | Yes | **Recommended** - most reliable |
| REAL | Yes | 32-bit float |
| LREAL | Yes | 64-bit float (S7-1500) |
| STRING | Yes | Auto-chunked for large strings |
| WSTRING | Yes | Auto-chunked for large strings |
| Arrays | Yes | Scalar types, auto-chunked |

**S7 Requirements:**
- PUT/GET communication must be enabled in PLC settings
- Optimized block access must be disabled on data blocks
- Block must not be write-protected

**S7 Notes:**
- Large writes (STRING, WSTRING, arrays) are automatically split into PDU-sized chunks
- String arrays (`[]string`) are not supported for writes

### Beckhoff TwinCAT (ADS)

| Type | Write Support | Notes |
|------|---------------|-------|
| BOOL | Yes | |
| BYTE/USINT | Yes | 8-bit |
| SINT | Yes | 8-bit signed |
| WORD/UINT | Yes | 16-bit |
| INT | Yes | 16-bit signed |
| DWORD/UDINT | Yes | 32-bit |
| DINT | Yes | **Recommended** |
| LINT/ULINT | Yes | 64-bit |
| REAL | Yes | 32-bit float |
| LREAL | Yes | 64-bit float |
| STRING | Yes | |
| WSTRING | Yes | UTF-16 |
| TIME/LTIME | Yes | Duration types |
| Arrays | Yes | All scalar types |

### Omron (CS/CJ/CP via FINS, NJ/NX via EtherNet/IP)

| Type | Write Support | Notes |
|------|---------------|-------|
| BOOL | Yes | Bit access |
| WORD | Yes | 16-bit |
| DWORD | Yes | 32-bit |
| INT | Yes | 16-bit signed |
| DINT | Yes | **Recommended** |
| REAL | Yes | 32-bit float |
| LREAL | Yes | 64-bit float (NJ/NX) |
| Arrays | Yes | Consecutive words |

---

## Marking Tags as Writable

In the Browser tab, press `w` on a tag to toggle the writable flag. Alternatively, set `writable: true` in config.yaml for pre-configured tags.

## Write-Back via Services

Tags can also be written via REST API, MQTT, Valkey, and Kafka. See the respective documentation for write request formats.

---

## Best Practices

### Use DINT for Status Codes

DINT (32-bit signed integer) is the most reliable type across all PLC families. Use values like 0=Idle, 1=Success, -1=Error.

### Dedicated Tags Only

Write only to tags that are exclusively controlled by WarLogix:

```
GOOD: WarLogix_AckCode      (dedicated to WarLogix)
BAD:  ProcessSetpoint       (used by PLC logic)
```

### Verify Before Writing

Always read the current value before writing if you need to confirm the write succeeded:

1. Read current value
2. Write new value
3. Read again to verify (optional)

### Error Handling

Writes may fail due to:
- Network issues
- PLC in stop mode
- Tag doesn't exist
- Type mismatch
- Access denied

The TUI shows write errors in the status bar. Check the debug log (`--log-debug=<family>`) for details.

---

## Limitations

- **Not real-time** - Writes go through network stack with variable latency
- **No guaranteed delivery** - Network issues can cause silent failures
- **Single-threaded** - High write rates may block reads
- **No atomicity** - Multiple tag writes are not transactional

For applications requiring guaranteed, time-critical writes, use dedicated industrial protocols directly from your control system.
