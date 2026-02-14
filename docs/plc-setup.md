# PLC Setup Guide

This guide covers PLC-specific configuration, capabilities, and troubleshooting for each supported PLC family.

## Supported PLCs Overview

| Family | Models | Tag Discovery | Protocol | Batching |
|--------|--------|---------------|----------|----------|
| **Allen-Bradley** | ControlLogix (1756), CompactLogix (1769), Micro800 | Automatic | EtherNet/IP (CIP) | Yes (MSP) |
| **Siemens** | S7-300/400/1200/1500 | Manual | S7comm | Yes (PDU) |
| **Beckhoff** | TwinCAT 2/3 | Automatic | ADS | Yes (SumUp) |
| **Omron FINS** | CS1, CJ1/2, CP1, CV | Manual | FINS/TCP, FINS/UDP | Yes (Multi-read) |
| **Omron EIP** | NJ, NX1/102/502/702 | Automatic (no UDT members) | EtherNet/IP (CIP) | Yes (MSP) |

---

## Allen-Bradley ControlLogix/CompactLogix

### PLC-Side Setup

No special configuration required - EtherNet/IP is enabled by default.

**Requirements:**
- PLC has an IP address (via RSLogix/Studio 5000 or DHCP)
- TCP port 44818 accessible from WarLink host

### WarLink Configuration

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

### WarLink Configuration

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

## Siemens S7 Rack/Slot Configuration

The S7 driver defaults to **Rack 0, Slot 2** (common for S7-300/400). Configure the slot based on your PLC model:

| PLC Model | Rack | Slot | Notes |
|-----------|------|------|-------|
| S7-300 | 0 | 2 | CPU typically in slot 2 (default) |
| S7-400 | 0 | 2-4 | Varies by chassis configuration |
| S7-1200 | 0 | 0 | Integrated CPU (onboard) |
| S7-1500 | 0 | 0 | Integrated CPU (onboard) |

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

### WarLink Configuration

```yaml
- name: SiemensPLC
  address: 192.168.1.102
  family: s7
  slot: 0                 # IMPORTANT: Use 0 for S7-1200/1500 (default is 2)
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
| ISO Invalid Buffer | Wrong slot - use slot 0 for S7-1200/1500 |

---

## Siemens S7-300/400

### PLC-Side Setup

No special configuration typically required.

### WarLink Configuration

```yaml
- name: S7-300
  address: 192.168.1.103
  family: s7
  # slot: 2              # Default is 2, omit if using slot 2
  enabled: true
```

### Notes

- **Slot 2 is the default** - no need to specify for most S7-300 installations
- S7-400 slot varies by chassis configuration (check where CPU is installed)
- Uses same addressing format as S7-1200/1500

---

## Beckhoff TwinCAT

### PLC-Side Setup

1. **Note your AMS Net ID** (in TwinCAT System Manager, usually `<IP>.1.1`)
2. **Add a route** for the WarLink machine (see below)
3. **Open firewall** for TCP port 48898
4. **PLC must be in RUN mode** for symbol access

### Adding a Route (Required)

Since WarLink runs without a local TwinCAT installation, you must add a route:

1. Open TwinCAT XAE and connect to your PLC
2. Go to **SYSTEM > Routes** and click **Add**
3. Configure:
   - **Route Name:** WarLink
   - **AMS Net Id:** Your machine's IP + `.1.1` (e.g., `192.168.1.50.1.1`)
   - **Transport Type:** TCP/IP
   - **Address Info:** Your machine's IP (e.g., `192.168.1.50`)
   - **Target Route:** Static
   - **Remote Route:** None / Server
   - Do **not** select Secure ADS
4. Click **Add Route**

### WarLink Configuration

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
| No route | Add route in TwinCAT for WarLink machine |
| Port not found | Check AMS port (851 vs 801) |
| No symbols | Ensure PLC is in RUN mode with project activated |
| Security error | TwinCAT 3.1 Build 4024+ has stricter defaults |

---

## Omron FINS (CS/CJ/CP Series)

### Overview

FINS (Factory Interface Network Service) is Omron's proprietary protocol for older PLC series. WarLink supports both FINS/TCP and FINS/UDP transports with automatic fallback and optimized batching.

**Supported PLC Series:**
| Series | Models | Transport | Notes |
|--------|--------|-----------|-------|
| **CS1** | CS1G, CS1H, CS1D | TCP, UDP | Duplex models supported |
| **CJ1** | CJ1G, CJ1H, CJ1M | TCP, UDP | Most common series |
| **CJ2** | CJ2H, CJ2M | TCP, UDP | Enhanced performance |
| **CP1** | CP1E, CP1H, CP1L | UDP | Compact series |
| **CV** | CV500, CV1000, CV2000 | UDP | Legacy series |

### PLC-Side Setup

1. **Configure IP address** via CX-Programmer, rotary switches, or web interface
2. **FINS port** is typically 9600 (default for both TCP and UDP)
3. **Note the node address** (often matches last IP octet, or set via rotary switches)
4. **Open firewall** for TCP and UDP port 9600

### WarLink Configuration

```yaml
- name: OmronPLC
  address: 192.168.1.105
  family: omron
  # protocol: fins         # Default - uses FINS (auto TCP/UDP)
  fins_port: 9600          # Port (default: 9600)
  fins_node: 0             # FINS destination node number
  fins_network: 0          # FINS network number (0 = local)
  fins_unit: 0             # CPU unit number
  enabled: true
  tags:
    - name: DM100
      alias: MotorSpeed
      data_type: DINT
      enabled: true
    - name: DM104
      alias: Temperature
      data_type: REAL
      enabled: true
    - name: CIO50
      alias: OutputStatus
      data_type: WORD
      enabled: true
```

### Transport Selection

WarLink automatically selects the optimal transport:

| Transport | When Used | Advantages |
|-----------|-----------|------------|
| **FINS/TCP** | Tried first (default) | Reliable, better error handling, persistent connection |
| **FINS/UDP** | Fallback if TCP fails | Works with older PLCs, lower overhead |

To force a specific transport, set `protocol: fins-tcp` or `protocol: fins-udp`.

### Tag Addressing

Tags must be configured manually with memory area and address:

| Memory Area | Format | Example | Description |
|-------------|--------|---------|-------------|
| DM | `DM<addr>` | `DM100` | Data Memory |
| CIO | `CIO<addr>` | `CIO50` | Core I/O |
| WR | `WR<addr>` | `WR10` | Work Area |
| HR | `HR<addr>` | `HR0` | Holding Area |
| AR | `AR<addr>` | `AR0` | Auxiliary Area |
| EM | `EM<bank>:<addr>` | `EM0:100` | Extended Memory |

**Bit access:** `DM100.5` (bit 5 of DM100)
**Arrays:** `DM100[10]` (10 consecutive words starting at DM100)

**Important:** Always specify `data_type` for FINS tags.

### Performance Optimization

WarLink implements several FINS optimizations:

1. **Contiguous Address Grouping** - Sequential addresses in the same memory area are read in a single bulk request (up to 998 words per request)

2. **Multi-Memory Area Read** - Non-contiguous addresses are batched using FINS command 0x0104, reading up to 64 memory areas per request

3. **Automatic TCP/UDP Selection** - TCP is preferred for reliability; UDP is used as fallback

**Example optimization:** Reading `DM100, DM101, DM102, DM103, DM200, CIO50`:
- **Old behavior:** 6 individual requests
- **New behavior:** 2 requests (DM100-103 as bulk, DM200+CIO50 via multi-read)

### Troubleshooting

| Issue | Solution |
|-------|----------|
| Timeout | Check IP, port, and PLC power |
| Wrong node | Verify FINS node matches PLC rotary switch or config |
| No response | Ensure port 9600 not blocked (check both TCP and UDP) |
| TCP refused, UDP works | Some older PLCs only support UDP |
| Wrong values | Check memory area and address; verify data_type |
| Parameter error (0x1103) | Address out of range for memory area |

---

## Omron EIP/CIP (NJ/NX Series)

### Overview

Omron NJ and NX series PLCs support EtherNet/IP with CIP (Common Industrial Protocol), providing symbolic tag addressing and automatic tag discovery. WarLink discovers tag names, data types, and array dimensions automatically, but does not currently unpack UDT/structure members — structures appear as opaque `STRUCT_XX` types. High-performance batching and connected messaging are implemented for optimal throughput.

**Supported PLC Series:**
| Series | Models | Features |
|--------|--------|----------|
| **NJ** | NJ101, NJ301, NJ501 | Motion + logic, EtherNet/IP built-in |
| **NX1** | NX1P2 | Compact all-in-one |
| **NX102** | NX102 | Mid-range CPU |
| **NX502** | NX502 | High-performance CPU |
| **NX702** | NX702 | High-end CPU with safety |
| **NA** | NA5 (HMI) | HMI with tag server |

### PLC-Side Setup

1. **Configure IP address** via Sysmac Studio
2. **Enable EtherNet/IP** on the built-in port (enabled by default on most models)
3. **Open firewall** for TCP port 44818 (EtherNet/IP)
4. **Publish tags** - tags must be published/exposed for external access in Sysmac Studio

### WarLink Configuration

```yaml
- name: OmronNJ
  address: 192.168.1.110
  family: omron
  protocol: eip           # Use EtherNet/IP instead of FINS
  enabled: true
  tags:
    - name: ProductCount
      enabled: true
    - name: MotorSpeed
      enabled: true
    - name: AlarmStatus
      enabled: true
```

### Key Differences from FINS

| Feature | FINS (CS/CJ/CP) | EIP (NJ/NX) |
|---------|-----------------|-------------|
| **Tag Addressing** | Memory addresses (DM100, CIO50) | Symbolic names (ProductCount) |
| **Tag Discovery** | Manual configuration | Automatic (tags and types, but not UDT members) |
| **Data Types** | Explicit `data_type` required | Embedded in tag metadata |
| **Port** | TCP/UDP 9600 | TCP 44818 |
| **Byte Order** | Big-endian | Little-endian |
| **Batching** | Multi-memory read (0x0104) | Multiple Service Packet (MSP) |

### Tag Addressing

EIP uses symbolic tag names defined in the Sysmac Studio project:

```yaml
tags:
  - name: MyVariable          # Simple variable
    enabled: true
  - name: MyArray[0]          # Array element
    enabled: true
  - name: MyStruct.Member     # Structure member
    enabled: true
```

**Note:** Tag names are case-sensitive and must match exactly as defined in Sysmac Studio.

### Performance Optimization

WarLink implements several EIP/CIP optimizations for NJ/NX series:

1. **Efficient Tag Discovery** - Uses CIP Get Instance Attribute List (service 0x55) with pagination to discover tags in batches, instead of instance-by-instance queries. This significantly reduces discovery time for projects with many tags.

2. **Multiple Service Packet (MSP) Batching** - Multiple tag reads are combined into single CIP requests using service 0x0A, reading up to 50 tags per request in connected mode (20 in unconnected mode)

3. **Connected Messaging (Forward Open)** - Establishes a CIP connection for efficient persistent communication with larger payload sizes (up to 4002 bytes vs 504 bytes unconnected)

3. **Automatic Fallback** - If batched reads fail, individual tag reads are used automatically

**Example optimization:** Reading 50 tags:
- **Old behavior:** 50 individual CIP requests
- **New behavior:** 1 Multiple Service Packet request

### Connected Messaging

For maximum performance, WarLink can establish a CIP Forward Open connection:

```yaml
- name: OmronNJ
  address: 192.168.1.110
  family: omron
  protocol: eip
  use_connected: true     # Enable Forward Open (optional)
  enabled: true
```

| Mode | Batch Size | Payload Size | Throughput |
|------|------------|--------------|------------|
| Unconnected | 20 tags | 504 bytes | 200-500 tags/sec |
| Connected | 50 tags | 4002 bytes | 500-2,000 tags/sec |

### Troubleshooting

| Issue | Solution |
|-------|----------|
| Connection refused | Ensure port 44818 is open and EtherNet/IP is enabled |
| Tag not found | Verify tag name matches exactly (case-sensitive) |
| Discovery empty | Check that tags are published/exposed in Sysmac Studio |
| Forward Open rejected | PLC may not support large connections; falls back to unconnected |
| Timeout | Check network connectivity and PLC power |
| MSP partial failure | Some tags in batch failed; check individual tag names |

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
| Omron EIP | Little-endian |
| Allen-Bradley | Little-endian |
| Beckhoff | Little-endian |

WarLink automatically handles byte order conversion for known types. Unknown types (UDTs without templates) are returned as raw byte arrays in the PLC's native order.

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

---

## Debugging Connection Issues

For troubleshooting PLC communication problems, use the `--log-debug` flag:

```bash
./warlink --log-debug
```

This creates a `debug.log` file with detailed protocol information:

- Connection/disconnection events with parameters
- Raw protocol bytes (hex dumps) for TX/RX packets
- Parsed request/response details
- Error codes and descriptions

**Example debug output for S7:**
```
[S7] Connecting to 192.168.1.102 (rack=0, slot=0)
[S7] TX: 03 00 00 16 11 e0 00 00 00 01 00 c0 01 0a c1 02 01 00 c2 02 01 00
[S7] RX: 03 00 00 16 11 d0 00 01 00 01 00 c0 01 0a c1 02 01 00 c2 02 01 00
[S7] Read "DB1.0": area=DB db=1 offset=0 type=DINT elemSize=4 count=1
```

This is invaluable for diagnosing:
- Incorrect slot/rack configuration
- Network timeout issues
- Protocol errors from the PLC
- Data type mismatches
