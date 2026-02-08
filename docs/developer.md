# Developer Guide

This guide covers how to use WarLogix's underlying drivers in your own Go applications. Each PLC driver can be imported and used independently.

## Driver Interface

All PLC drivers implement a common interface defined in `driver/interface.go`:

```go
type Driver interface {
    // Connection management
    Connect() error
    Close() error
    IsConnected() bool

    // Identification
    Family() config.PLCFamily
    ConnectionMode() string
    GetDeviceInfo() (*DeviceInfo, error)

    // Tag discovery (not all families support this)
    SupportsDiscovery() bool
    AllTags() ([]TagInfo, error)
    Programs() ([]string, error)

    // Read/Write operations
    Read(requests []TagRequest) ([]*TagValue, error)
    Write(tag string, value interface{}) error

    // Maintenance
    Keepalive() error
    IsConnectionError(err error) bool
}
```

## Common Types

### TagRequest

```go
type TagRequest struct {
    Name     string // Tag name or address
    TypeHint string // Optional type hint (e.g., "INT", "REAL", "DINT")
}
```

### TagValue

```go
type TagValue struct {
    Name     string      // Tag name
    DataType uint16      // Native type code (family-specific)
    Family   string      // PLC family identifier
    Value    interface{} // Decoded Go value
    Bytes    []byte      // Raw bytes in native byte order
    Count    int         // Element count (1 for scalar)
    Error    error       // Per-tag error (nil if successful)
}
```

### TagInfo

```go
type TagInfo struct {
    Name       string   // Tag name
    TypeCode   uint16   // Native type code
    Instance   uint32   // CIP instance ID
    Dimensions []uint32 // Array dimensions
    TypeName   string   // Human-readable type name
    Writable   bool     // Whether the tag can be written
}
```

### DeviceInfo

```go
type DeviceInfo struct {
    Family       config.PLCFamily
    Vendor       string
    Model        string
    Version      string
    SerialNumber string
    Description  string
}
```

---

## Allen-Bradley Logix Driver

**Package:** `warlogix/logix`

Supports ControlLogix, CompactLogix, and Micro800 series PLCs via EtherNet/IP.

### Basic Usage

```go
package main

import (
    "fmt"
    "log"

    "warlogix/logix"
)

func main() {
    // Connect to PLC
    client, err := logix.Connect("192.168.1.100", logix.WithSlot(0))
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Read a tag
    value, err := client.Read("Program:MainProgram.Counter")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Counter = %v\n", value.Value)

    // Write a tag
    err = client.Write("Program:MainProgram.SetPoint", 100.5)
    if err != nil {
        log.Fatal(err)
    }
}
```

### Connection Options

```go
// ControlLogix with specific slot
client, _ := logix.Connect("192.168.1.100", logix.WithSlot(2))

// Micro800 (no slot routing, unconnected messaging)
client, _ := logix.Connect("192.168.1.101", logix.WithMicro800())

// Custom route path (for routing through gateways)
client, _ := logix.Connect("192.168.1.100", logix.WithRoutePath([]byte{0x01, 0x00, 0x02, 0x00}))

// Skip Forward Open (use unconnected messaging only)
client, _ := logix.Connect("192.168.1.100", logix.WithoutConnection())
```

### Tag Discovery

```go
// Get all tags
tags, err := client.AllTags()
if err != nil {
    log.Fatal(err)
}

for _, tag := range tags {
    fmt.Printf("%s: %s (type=0x%04X)\n", tag.Name, tag.TypeName, tag.TypeCode)
}

// Get program list
programs, err := client.Programs()
for _, prog := range programs {
    fmt.Println("Program:", prog)
}
```

### Batch Reading

```go
// Read multiple tags efficiently (batched into single request)
tags := []string{"Tag1", "Tag2", "Tag3", "Program:Main.Counter"}
values, err := client.ReadMultiple(tags)
if err != nil {
    log.Fatal(err)
}

for _, val := range values {
    if val.Error != nil {
        fmt.Printf("%s: error %v\n", val.Name, val.Error)
    } else {
        fmt.Printf("%s = %v\n", val.Name, val.Value)
    }
}
```

### Device Discovery

```go
// Discover PLCs on the network
devices, err := logix.Discover("255.255.255.255", 3*time.Second)
if err != nil {
    log.Fatal(err)
}

for _, dev := range devices {
    fmt.Printf("Found: %s at %s\n", dev.ProductName, dev.IP)
}
```

### UDT/Structure Support

```go
// Read a UDT - returns as map[string]interface{}
value, err := client.Read("MyUDT")
if err != nil {
    log.Fatal(err)
}

// Access as map
if m, ok := value.Value.(map[string]interface{}); ok {
    fmt.Printf("Temperature: %v\n", m["Temperature"])
    fmt.Printf("Running: %v\n", m["Running"])
}

// Get template for a UDT type
template, err := client.GetTemplate(value.DataType)
if err == nil {
    for _, member := range template.Members {
        fmt.Printf("  %s: %s\n", member.Name, logix.TypeName(member.Type))
    }
}
```

---

## Siemens S7 Driver

**Package:** `warlogix/s7`

Supports S7-300, S7-400, S7-1200, and S7-1500 PLCs via S7comm.

### Basic Usage

```go
package main

import (
    "fmt"
    "log"

    "warlogix/s7"
)

func main() {
    // Connect to S7-1500 (slot 0)
    client, err := s7.Connect("192.168.1.102", 0, 0)
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Read a DINT from DB1 at byte offset 0
    value, err := client.Read("DB1.DBD0", "DINT")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Value = %v\n", value.Value)

    // Write a REAL to DB1 at byte offset 4
    err = client.Write("DB1.DBD4", float32(72.5))
    if err != nil {
        log.Fatal(err)
    }
}
```

### Address Formats

| Format | Example | Description |
|--------|---------|-------------|
| `DB<n>.DB<type><offset>` | `DB1.DBD0` | Data block DWORD at offset 0 |
| `DB<n>.DBX<offset>.<bit>` | `DB1.DBX4.0` | Data block bit |
| `I<offset>` | `IB0` | Input byte |
| `Q<offset>` | `QB0` | Output byte |
| `M<offset>` | `MB0` | Marker byte |

Type suffixes: `X` (bit), `B` (byte), `W` (word), `D` (dword)

### Connection Parameters

```go
// S7-300/400 (typically slot 2)
client, _ := s7.Connect("192.168.1.100", 0, 2)

// S7-1200/1500 (always slot 0)
client, _ := s7.Connect("192.168.1.100", 0, 0)

// With custom rack
client, _ := s7.Connect("192.168.1.100", 1, 2) // Rack 1, Slot 2
```

### Batch Reading

```go
// Read multiple addresses (automatically batched by PDU size)
requests := []s7.ReadRequest{
    {Address: "DB1.DBD0", DataType: "DINT"},
    {Address: "DB1.DBD4", DataType: "REAL"},
    {Address: "DB1.DBD8", DataType: "DINT"},
}

values, err := client.ReadMultiple(requests)
if err != nil {
    log.Fatal(err)
}

for _, val := range values {
    fmt.Printf("%s = %v\n", val.Name, val.Value)
}
```

### String Reading

```go
// Read a STRING (S7 format: 2-byte header + chars)
value, err := client.Read("DB10.DBB0", "STRING")
fmt.Printf("String: %s\n", value.Value)

// Read a WSTRING (wide string)
value, err = client.Read("DB10.DBB100", "WSTRING")
```

---

## Beckhoff ADS Driver

**Package:** `warlogix/ads`

Supports TwinCAT 2 and TwinCAT 3 PLCs via ADS protocol.

### Basic Usage

```go
package main

import (
    "fmt"
    "log"

    "warlogix/ads"
)

func main() {
    // Connect to TwinCAT 3
    client, err := ads.Connect("192.168.1.103", "192.168.1.103.1.1", 851)
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Read a variable
    value, err := client.Read("MAIN.Counter")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Counter = %v\n", value.Value)

    // Write a variable
    err = client.Write("MAIN.SetPoint", float32(100.0))
    if err != nil {
        log.Fatal(err)
    }
}
```

### Connection Parameters

```go
// TwinCAT 3 (port 851)
client, _ := ads.Connect("192.168.1.103", "192.168.1.103.1.1", 851)

// TwinCAT 2 (port 801)
client, _ := ads.Connect("192.168.1.103", "192.168.1.103.1.1", 801)
```

### Symbol Discovery

```go
// Get all symbols
symbols, err := client.AllSymbols()
if err != nil {
    log.Fatal(err)
}

for _, sym := range symbols {
    fmt.Printf("%s: %s\n", sym.Name, sym.TypeName)
}
```

### Batch Reading (SumUp Read)

```go
// Read multiple symbols efficiently using SumUp Read
// This batches all reads into a single request (0xF080)
symbols := []string{"MAIN.Var1", "MAIN.Var2", "GVL.GlobalCounter"}
values, err := client.ReadMultiple(symbols)
if err != nil {
    log.Fatal(err)
}

for _, val := range values {
    fmt.Printf("%s = %v\n", val.Name, val.Value)
}
```

### Notifications (Subscriptions)

```go
// Subscribe to variable changes
handle, err := client.Subscribe("MAIN.Counter", 100*time.Millisecond, func(value *ads.Value) {
    fmt.Printf("Counter changed: %v\n", value.Value)
})
if err != nil {
    log.Fatal(err)
}

// Later, unsubscribe
client.Unsubscribe(handle)
```

---

## Omron FINS Driver

**Package:** `warlogix/omron`

Supports CS/CJ/CP series PLCs via FINS/UDP protocol.

### Basic Usage

```go
package main

import (
    "fmt"
    "log"

    "warlogix/omron"
)

func main() {
    // Connect to Omron PLC
    client, err := omron.Connect("192.168.1.104", 9600, 0, 0, 0)
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Read DM100 as DINT
    value, err := client.Read("DM100", "DINT")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("DM100 = %v\n", value.Value)

    // Write to DM200
    err = client.Write("DM200", int32(1234))
    if err != nil {
        log.Fatal(err)
    }
}
```

### Memory Areas

| Area | Format | Description |
|------|--------|-------------|
| DM | `DM<addr>` | Data Memory |
| CIO | `CIO<addr>` | Core I/O |
| WR | `WR<addr>` | Work Area |
| HR | `HR<addr>` | Holding Area |
| AR | `AR<addr>` | Auxiliary Area |

Bit access: `DM100.5` (bit 5 of DM100)
Arrays: `DM100[10]` (10 consecutive words starting at DM100)

### Connection Parameters

```go
// Standard connection
client, _ := omron.Connect(
    "192.168.1.104", // IP address
    9600,            // FINS UDP port
    0,               // Network number (0 = local)
    0,               // Node number
    0,               // Unit number
)
```

---

## Omron EIP Driver (NJ/NX Series)

For Omron NJ/NX series, use EtherNet/IP with symbolic tags:

```go
package main

import (
    "fmt"
    "log"

    "warlogix/omron"
)

func main() {
    // Connect via EtherNet/IP (port 44818)
    client, err := omron.ConnectEIP("192.168.1.105")
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Tag discovery
    tags, err := client.AllTags()
    for _, tag := range tags {
        fmt.Printf("%s: %s\n", tag.Name, tag.TypeName)
    }

    // Read by symbolic name
    value, err := client.Read("ProductCount")
    fmt.Printf("ProductCount = %v\n", value.Value)
}
```

---

## Using the PLC Manager

For applications that need to manage multiple PLCs, use `plcman.Manager`:

```go
package main

import (
    "fmt"
    "log"
    "time"

    "warlogix/config"
    "warlogix/plcman"
)

func main() {
    // Create manager
    mgr := plcman.NewManager()

    // Add PLCs from configuration
    cfg := &config.PLCConfig{
        Name:    "MainPLC",
        Address: "192.168.1.100",
        Family:  config.FamilyLogix,
        Slot:    0,
        Enabled: true,
    }
    mgr.AddPLC(cfg)

    // Connect
    err := mgr.Connect("MainPLC")
    if err != nil {
        log.Fatal(err)
    }

    // Start polling
    mgr.StartPolling(500 * time.Millisecond)
    defer mgr.StopPolling()

    // Register callback for value changes
    mgr.OnValueChange(func(plcName, tagName string, value *plcman.TagValue) {
        fmt.Printf("%s.%s = %v\n", plcName, tagName, value.GoValue())
    })

    // Enable specific tags for polling
    mgr.EnableTag("MainPLC", "Program:Main.Counter", true)
    mgr.EnableTag("MainPLC", "Program:Main.Temperature", true)

    // Read a single tag
    value, err := mgr.ReadTag("MainPLC", "Program:Main.Counter")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Counter = %v\n", value.GoValue())

    // Write a tag
    err = mgr.WriteTag("MainPLC", "Program:Main.SetPoint", 100.0)
    if err != nil {
        log.Fatal(err)
    }

    // Keep running...
    select {}
}
```

---

## Publishing to Brokers

### MQTT Publisher

```go
package main

import (
    "warlogix/config"
    "warlogix/mqtt"
)

func main() {
    cfg := &config.MQTTConfig{
        Name:      "broker1",
        Broker:    "localhost",
        Port:      1883,
        RootTopic: "factory",
        ClientID:  "myapp",
    }

    pub := mqtt.NewPublisher(cfg)
    if err := pub.Start(); err != nil {
        log.Fatal(err)
    }
    defer pub.Stop()

    // Publish a tag value
    msg := &mqtt.TagMessage{
        PLC:       "MainPLC",
        Tag:       "Counter",
        Value:     42,
        Type:      "DINT",
        Timestamp: time.Now(),
    }
    pub.Publish(msg)
}
```

### Kafka Publisher

```go
package main

import (
    "warlogix/kafka"
)

func main() {
    cfg := &kafka.Config{
        Name:    "cluster1",
        Brokers: []string{"localhost:9092"},
        Topic:   "plc-tags",
    }

    mgr := kafka.NewManager()
    mgr.AddCluster(cfg)

    if err := mgr.Connect("cluster1"); err != nil {
        log.Fatal(err)
    }
    defer mgr.DisconnectAll()

    // Publish a message
    msg := &kafka.TagMessage{
        PLC:       "MainPLC",
        Tag:       "Counter",
        Value:     42,
        Type:      "DINT",
        Timestamp: time.Now(),
    }
    mgr.Publish("cluster1", "plc-tags", msg)
}
```

### Valkey/Redis Publisher

```go
package main

import (
    "warlogix/config"
    "warlogix/valkey"
)

func main() {
    cfg := &config.ValkeyConfig{
        Name:           "redis1",
        Address:        "localhost:6379",
        Factory:        "factory",
        PublishChanges: true,
    }

    mgr := valkey.NewManager()
    pub := mgr.Add(cfg)

    if err := pub.Start(); err != nil {
        log.Fatal(err)
    }
    defer pub.Stop()

    // Store a tag value
    msg := &valkey.TagMessage{
        PLC:       "MainPLC",
        Tag:       "Counter",
        Value:     42,
        Type:      "DINT",
        Timestamp: time.Now(),
    }
    pub.Publish(msg)
}
```

---

## Error Handling

All drivers return errors that can be inspected for connection issues:

```go
value, err := client.Read("SomeTag")
if err != nil {
    // Check if it's a connection error (may need reconnect)
    if driver.IsConnectionError(err) {
        log.Println("Connection lost, reconnecting...")
        client.Close()
        client, _ = logix.Connect(address, opts...)
    } else {
        // Tag-specific error
        log.Printf("Read error: %v\n", err)
    }
}

// For batch reads, check per-tag errors
values, err := client.ReadMultiple(tags)
if err != nil {
    // Overall request failed
    log.Fatal(err)
}
for _, val := range values {
    if val.Error != nil {
        log.Printf("%s: %v\n", val.Name, val.Error)
    }
}
```

---

## Performance Tips

1. **Use batch reads** - ReadMultiple() is much faster than individual Read() calls
2. **Maintain connections** - Reuse client instances instead of reconnecting
3. **Use connected messaging** - For Logix, Forward Open reduces latency
4. **Set appropriate poll rates** - 250ms minimum, 500ms-1s typical
5. **Filter changes** - Only process values that actually changed
6. **Use keepalive** - Call Keepalive() periodically for idle connections

```go
// Keepalive example
ticker := time.NewTicker(30 * time.Second)
go func() {
    for range ticker.C {
        if err := client.Keepalive(); err != nil {
            log.Printf("Keepalive failed: %v\n", err)
        }
    }
}()
```

---

## Dependencies

Each driver package has minimal dependencies:

| Package | External Dependencies |
|---------|----------------------|
| `warlogix/logix` | None (pure Go) |
| `warlogix/s7` | `github.com/robinson/gos7` |
| `warlogix/ads` | None (pure Go) |
| `warlogix/omron` | `github.com/xiaotushaoxia/fins` |
| `warlogix/mqtt` | `github.com/eclipse/paho.mqtt.golang` |
| `warlogix/kafka` | `github.com/segmentio/kafka-go` |
| `warlogix/valkey` | `github.com/redis/go-redis/v9` |
