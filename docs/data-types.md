# Data Types and UDT Support

## Supported Data Types

| Type | Size | Description | JSON Type |
|------|------|-------------|-----------|
| BOOL | 1 bit | Boolean | `true`/`false` |
| SINT | 1 byte | 8-bit signed integer | number |
| USINT/BYTE | 1 byte | 8-bit unsigned integer | number |
| INT | 2 bytes | 16-bit signed integer | number |
| UINT/WORD | 2 bytes | 16-bit unsigned integer | number |
| DINT | 4 bytes | 32-bit signed integer | number |
| UDINT/DWORD | 4 bytes | 32-bit unsigned integer | number |
| LINT | 8 bytes | 64-bit signed integer | number |
| ULINT/LWORD | 8 bytes | 64-bit unsigned integer | number |
| REAL | 4 bytes | 32-bit float | number |
| LREAL | 8 bytes | 64-bit float | number |
| STRING | Variable | Text string | string |

## Arrays

Arrays of known types are published as JSON arrays:

```json
{
  "tag": "Temperatures",
  "value": [72.5, 73.1, 71.8, 72.0],
  "type": "REAL[4]"
}
```

Multi-dimensional arrays are flattened to a single dimension.

## Byte Order

Different PLC families use different byte orders:

| PLC Family | Byte Order |
|------------|------------|
| Siemens S7 | Big-endian |
| Omron FINS | Big-endian |
| Allen-Bradley Logix | Little-endian |
| Beckhoff TwinCAT | Little-endian |

**Known types** are automatically converted - you'll see the same numeric value regardless of PLC family.

**Unknown types** (UDTs without templates, raw structures) are returned as byte arrays in the PLC's native order:

```json
{
  "tag": "UnknownStruct",
  "value": [120, 86, 52, 18],
  "type": "Unknown"
}
```

To decode manually:
- **Big-endian (S7/Omron):** Most significant byte first
- **Little-endian (Logix/Beckhoff):** Least significant byte first

## UDT/Structure Support

WarLogix automatically unpacks UDT (User-Defined Type) members when the template is known on supported PLCs. The entire UDT can be published as JSOn, or each member can be published separately using dot-notation (MachineStatus.Running).

**PLC Structure:**
```
MachineStatus (UDT)
├── Running: BOOL
├── Speed: REAL
├── Counter: DINT
└── Timestamp: LINT
```

**Published as separate tags:**
- `MachineStatus.Running` = `true`
- `MachineStatus.Speed` = `1500.5`
- `MachineStatus.Counter` = `42`
- `MachineStatus.Timestamp` = `1705312200000`

## Change Detection Filtering

UDTs often contain volatile members (timestamps, heartbeats, counters) that change frequently but aren't meaningful for data capture. You can exclude these from change detection.

### Via Configuration

```yaml
tags:
  - name: Program:MainProgram.MachineStatus
    enabled: true
    ignore_changes: [Timestamp, HeartbeatCount, SequenceNum]
```

### Via Tag Browser

Press `i` on a UDT member to toggle ignore status. Ignored members show `[I]` indicator.

### Auto-Detection

When you enable a UDT tag, WarLogix automatically ignores common volatile types:
- TIMER, COUNTER
- TIME, LTIME
- DATE, DATE_AND_TIME
- TIME_OF_DAY, TOD, DT

### How It Works

- **Ignored members are still published** - they appear in the payload with current values
- **They don't trigger republishing** - if only ignored members change, no message is sent
- **Non-ignored changes trigger full publish** - the entire UDT (including ignored members) is republished

**Example:** A UDT with `Temperature` (not ignored) and `Timestamp` (ignored):
- Temperature changes from 72.0 to 72.5 → Message published with both values
- Only Timestamp changes → No message published
- Both change → Message published with both values

## Manual Tags (S7/Omron)

For PLCs without automatic discovery, specify `data_type`:

```yaml
tags:
  - name: DB1.0
    alias: ProductCount
    data_type: DINT
    enabled: true

  - name: DM100
    alias: SetPoint
    data_type: REAL
    enabled: true
```

### S7 Addressing

| Format | Example | Description |
|--------|---------|-------------|
| `DB<n>.<offset>` | `DB1.0` | Data block at byte offset |
| `DB<n>.<offset>.<bit>` | `DB1.4.0` | Bit within data block |
| `DB<n>.<offset>[count]` | `DB1.0[10]` | Array |
| `I<offset>` | `I0` | Input |
| `Q<offset>` | `Q0` | Output |
| `M<offset>` | `M0` | Marker |

### Omron Addressing

| Area | Format | Example |
|------|--------|---------|
| Data Memory | `DM<addr>` | `DM100` |
| Core I/O | `CIO<addr>` | `CIO50` |
| Work Area | `WR<addr>` | `WR10` |
| Holding | `HR<addr>` | `HR0` |
| Auxiliary | `AR<addr>` | `AR0` |

Bit access: `DM100.5` (bit 5)
Arrays: `DM100[10]` (10 words)

## Tag Aliases

Use `alias` to give address-based tags friendly names:

```yaml
- name: DB1.0          # Raw address
  alias: ProductCount  # Used in MQTT/Valkey/Kafka messages
  data_type: DINT
```

The alias appears in all published messages instead of the raw address.
