# PLC Setup Guide

This guide covers PLC-specific configuration, capabilities, and troubleshooting for each supported PLC family.

## Supported PLCs Overview

| Family | Models | Tag Discovery | Protocol |
|--------|--------|---------------|----------|
| **Allen-Bradley** | ControlLogix (1756), CompactLogix (1769), Micro800 | Automatic | EtherNet/IP |
| **Siemens** | S7-300/400/1200/1500 | Manual | S7comm |
| **Beckhoff** | TwinCAT 2/3 | Automatic | ADS |
| **Omron** | CJ/CS/CP/NJ/NX Series | Manual | FINS/UDP |

---

## Allen-Bradley ControlLogix/CompactLogix

### PLC-Side Setup

No special configuration required - EtherNet/IP is enabled by default.

**Requirements:**
- PLC has an IP address (via RSLogix/Studio 5000 or DHCP)
- TCP port 44818 accessible from WarLogix host

### WarLogix Configuration

```yaml
- name: MainPLC
  address: 192.168.1.100
  family: logix           # or omit (logix is default)
  slot: 0                 # CPU slot (typically 0 for CompactLogix)
  enabled: true
```

### Tag Addressing

Tags are discovered automatically. Use the Tag Browser to select which to publish.

**Program-scoped tags:** `Program:MainProgram.TagName`
**Controller-scoped tags:** `TagName`
**Array elements:** `TagName[0]`, `TagName[5,3]` (multi-dimensional)
**UDT members:** `MyUDT.Member`

### Troubleshooting

| Issue | Solution |
|-------|----------|
| Connection timeout | Verify IP and ping connectivity |
| Connection refused | Check firewall allows port 44818 |
| No tags discovered | Ensure PLC is in Run mode |
| Wrong slot | For ControlLogix, check CPU slot in chassis |

---

## Allen-Bradley Micro800

### PLC-Side Setup

Same as ControlLogix - EtherNet/IP enabled by default.

### WarLogix Configuration

```yaml
- name: Micro850
  address: 192.168.1.101
  family: micro800
  slot: 0
  enabled: true
```

### Notes

- Tag discovery is automatic
- Slot is typically 0
- Some older firmware may have limited tag support

---

## Siemens S7-1200/1500

### PLC-Side Setup (TIA Portal)

1. Open your project in TIA Portal
2. Select PLC > **Properties** > **Protection & Security**
3. Enable **Permit access with PUT/GET communication from remote partner**
4. For each Data Block:
   - Open DB properties > **Attributes**
   - Uncheck **Optimized block access** (required for absolute addressing)
5. Download project to PLC

### WarLogix Configuration

```yaml
- name: SiemensPLC
  address: 192.168.1.102
  family: s7
  slot: 0                 # Use 0 for S7-1200/1500 integrated CPU
  enabled: true
  tags:
    - name: DB1.0
      alias: ProductCount
      data_type: DINT
      enabled: true
    - name: DB1.4
      alias: Temperature
      data_type: REAL
      enabled: true
      writable: true
```

### Tag Addressing

Tags must be configured manually with byte offsets:

| Format | Example | Description |
|--------|---------|-------------|
| `DB<n>.<offset>` | `DB1.0` | Data block word |
| `DB<n>.<offset>.<bit>` | `DB1.4.0` | Data block bit |
| `DB<n>.<offset>[count]` | `DB1.0[10]` | Array of 10 elements |
| `I<offset>` | `I0` | Input byte |
| `Q<offset>` | `Q0` | Output byte |
| `M<offset>` | `M0` | Marker/memory byte |

**Important:** Always specify `data_type` for S7 tags.

### Troubleshooting

| Issue | Solution |
|-------|----------|
| Connection refused | PUT/GET not enabled in TIA Portal |
| Access denied | Data block has optimized access enabled |
| Wrong values | Check byte offsets match your DB layout |
| Timeout | Verify IP address and network connectivity |

---

## Siemens S7-300/400

### PLC-Side Setup

No special configuration typically required.

### WarLogix Configuration

```yaml
- name: S7-300
  address: 192.168.1.103
  family: s7
  slot: 2                 # CPU slot (typically 2 for S7-300)
  enabled: true
```

### Notes

- Slot is typically 2 for S7-300, varies for S7-400
- Uses same addressing format as S7-1200/1500

---

## Beckhoff TwinCAT

### PLC-Side Setup

1. **Note your AMS Net ID** (in TwinCAT System Manager, usually `<IP>.1.1`)
2. **Add a route** for the WarLogix machine (see below)
3. **Open firewall** for TCP port 48898
4. **PLC must be in RUN mode** for symbol access

### Adding a Route (Required)

Since WarLogix runs without a local TwinCAT installation, you must add a route:

1. Open TwinCAT XAE and connect to your PLC
2. Go to **SYSTEM > Routes** and click **Add**
3. Configure:
   - **Route Name:** WarLogix
   - **AMS Net Id:** Your machine's IP + `.1.1` (e.g., `192.168.1.50.1.1`)
   - **Transport Type:** TCP/IP
   - **Address Info:** Your machine's IP (e.g., `192.168.1.50`)
   - **Target Route:** Static
   - **Remote Route:** None / Server
   - Do **not** select Secure ADS
4. Click **Add Route**

### WarLogix Configuration

```yaml
- name: TwinCAT
  address: 192.168.1.104
  family: beckhoff
  ams_net_id: 192.168.1.104.1.1    # PLC's AMS Net ID
  ams_port: 851                     # 851 for TC3, 801 for TC2
  enabled: true
```

### Tag Addressing

Tags are discovered automatically from the symbol table.

| Format | Example | Description |
|--------|---------|-------------|
| `MAIN.Variable` | `MAIN.Counter` | Variable in MAIN program |
| `GVL.Variable` | `GVL.GlobalTemp` | Global Variable List |
| `FB.Member` | `fbMotor.Speed` | Function block member |

### Troubleshooting

| Issue | Solution |
|-------|----------|
| Connection reset | Route not configured or wrong AMS Net ID |
| No route | Add route in TwinCAT for WarLogix machine |
| Port not found | Check AMS port (851 vs 801) |
| No symbols | Ensure PLC is in RUN mode with project activated |
| Security error | TwinCAT 3.1 Build 4024+ has stricter defaults |

---

## Omron FINS

### PLC-Side Setup

1. **Configure IP address** via CX-Programmer, Sysmac Studio, or rotary switches
2. **FINS port** is typically UDP 9600 (default)
3. **Note the node address** (often matches last IP octet)
4. **Open firewall** for UDP port 9600

### WarLogix Configuration

```yaml
- name: OmronPLC
  address: 192.168.1.105
  family: omron
  fins_port: 9600         # UDP port (default: 9600)
  fins_node: 0            # FINS node number
  fins_network: 0         # FINS network number (0 = local)
  fins_unit: 0            # CPU unit number
  enabled: true
  tags:
    - name: DM100
      alias: MotorSpeed
      data_type: DINT
      enabled: true
    - name: CIO50
      alias: OutputStatus
      data_type: WORD
      enabled: true
```

### Tag Addressing

Tags must be configured manually with memory area and address:

| Memory Area | Format | Example | Description |
|-------------|--------|---------|-------------|
| DM | `DM<addr>` | `DM100` | Data Memory |
| CIO | `CIO<addr>` | `CIO50` | Core I/O |
| WR | `WR<addr>` | `WR10` | Work Area |
| HR | `HR<addr>` | `HR0` | Holding Area |
| AR | `AR<addr>` | `AR0` | Auxiliary Area |

**Bit access:** `DM100.5` (bit 5 of DM100)
**Arrays:** `DM100[10]` (10 consecutive words)

**Important:** Always specify `data_type` for Omron tags.

### Troubleshooting

| Issue | Solution |
|-------|----------|
| Timeout | Check IP, port, and PLC power |
| Wrong node | Verify FINS node matches PLC configuration |
| No response | Ensure UDP 9600 not blocked by firewall |
| Wrong values | Check memory area and address |

---

## Data Types

All PLC families support these common data types:

| Type | Size | Description |
|------|------|-------------|
| BOOL | 1 bit | Boolean true/false |
| SINT/BYTE | 1 byte | 8-bit signed/unsigned |
| INT/WORD | 2 bytes | 16-bit signed/unsigned |
| DINT/DWORD | 4 bytes | 32-bit signed/unsigned |
| LINT/LWORD | 8 bytes | 64-bit signed/unsigned |
| REAL | 4 bytes | 32-bit float |
| LREAL | 8 bytes | 64-bit float |
| STRING | Variable | Text string |

### Byte Order

| PLC Family | Byte Order |
|------------|------------|
| Siemens S7 | Big-endian |
| Omron FINS | Big-endian |
| Allen-Bradley | Little-endian |
| Beckhoff | Little-endian |

WarLogix automatically handles byte order conversion for known types. Unknown types (UDTs without templates) are returned as raw byte arrays in the PLC's native order.

---

## Tag Aliases

For PLCs with address-based tags (S7, Omron), use aliases for friendly names:

```yaml
tags:
  - name: DB1.0          # Raw address
    alias: ProductCount  # Friendly name used in MQTT/Valkey/Kafka
    data_type: DINT
    enabled: true
```

The alias appears in all published messages instead of the raw address.
