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

## Writing Values

WarLink supports writing values to PLC tags through the Browser tab. Press `W` on any writable tag to open the write dialog.

### Type-Aware Writes

When writing to a tag, WarLink uses the tag's actual data type discovered from the PLC. This ensures the correct CIP type code is sent, preventing type mismatch errors.

### Scalar Value Formats

| Type | Format | Examples |
|------|--------|----------|
| BOOL | `true` or `false` (case-insensitive) | `true`, `FALSE`, `True` |
| Integer types | Decimal number | `42`, `-100`, `65535` |
| REAL/LREAL | Decimal with optional fraction | `3.14`, `-0.5`, `100.0` |
| STRING | Plain text (no quotes needed) | `Hello World` |

### Array Value Formats

Arrays are written using bracket notation with space-separated or comma-separated values:

```
[value1 value2 value3]
[value1, value2, value3]
```

**Examples:**

| Type | Write Format |
|------|--------------|
| DINT[5] | `[1 2 3 4 5]` or `[1, 2, 3, 4, 5]` |
| BOOL[4] | `[true false true false]` |
| REAL[3] | `[1.5 2.5 3.5]` |
| STRING[3] | `[one two three]` |

### Quoted Strings in Arrays

For string arrays containing spaces, use double quotes around each element:

```
["one dog" "two dogs" "three dogs"]
```

You can mix quoted and unquoted strings:

```
["hello world" simple "another phrase" test]
```

**Note:** Quotes are only needed when string elements contain spaces. Simple strings work without quotes.

## Byte Order

Different PLC families use different byte orders:

| PLC Family | Protocol | Byte Order |
|------------|----------|------------|
| Siemens S7 | S7comm | Big-endian |
| Omron FINS | FINS TCP/UDP | Big-endian |
| Omron EIP | EtherNet/IP (CIP) | Little-endian |
| Allen-Bradley Logix | EtherNet/IP (CIP) | Little-endian |
| Beckhoff TwinCAT | ADS | Little-endian |

**Note:** Omron NJ/NX series using EtherNet/IP (CIP) are little-endian, matching Allen-Bradley. Older Omron PLCs using FINS are big-endian.

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

WarLink automatically unpacks UDT (User-Defined Type) members when the template is known on supported PLCs. You can publish the entire UDT as a JSON object, or publish individual members separately using dot-notation.

**PLC Structure:**
```
MachineStatus (UDT)
├── Running: BOOL
├── Speed: REAL
├── Counter: DINT
└── Timestamp: LINT
```

### Publishing Entire UDT as JSON Object

When you enable a UDT tag (e.g., `MachineStatus`), it is published as a nested JSON object:

```json
{
  "tag": "MachineStatus",
  "value": {
    "Running": true,
    "Speed": 1500.5,
    "Counter": 42,
    "Timestamp": 1705312200000
  },
  "type": "UDT",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

Nested UDTs are recursively unpacked into nested JSON objects.

### Publishing Individual Members

You can also enable individual UDT members using dot-notation in the Tag Browser. Each member is published as a separate tag:

- `MachineStatus.Running` = `true`
- `MachineStatus.Speed` = `1500.5`
- `MachineStatus.Counter` = `42`
- `MachineStatus.Timestamp` = `1705312200000`

This is useful when you only need specific members or want finer control over change detection.

## Change Detection Filtering

UDTs often contain volatile members (timestamps, heartbeats, counters) that change frequently but aren't meaningful for data capture. You can exclude these from change detection.

### Via Tag Browser

Press `i` on a UDT member to toggle ignore status. Ignored members show `[I]` indicator. You can also set `ignore_changes` in the tag configuration.

### Auto-Detection

When you enable a UDT tag, WarLink automatically ignores common volatile types:
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

## Manual Tags (S7/Omron FINS)

For PLCs without automatic discovery, add tags manually in the Browser tab (press `a`). Specify the address and data type when prompted.

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

For address-based PLCs (S7, Omron FINS), aliases give friendly names to memory addresses. When adding a manual tag, set an alias in the edit dialog. The alias appears in all published messages instead of the raw address.
