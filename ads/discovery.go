package ads

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"warlogix/logging"
)

// Discovery port for TwinCAT UDP broadcast
const DiscoveryUDPPort = 48899

// DiscoveredDevice contains identity information about a discovered Beckhoff/TwinCAT device.
type DiscoveredDevice struct {
	IP             net.IP // Device IP address
	Port           uint16 // ADS port (48898)
	AmsNetId       string // AMS Net ID if discovered
	ProductName    string // Product name if available
	Connected      bool   // True if successfully connected and identified
	HasRoute       bool   // True if route is configured (device responded to ADS request)
	Hostname       string // Device hostname if discovered
	TwinCATVersion string // TwinCAT version (e.g., "3.1.4024")
	OSVersion      string // Operating system version
	Fingerprint    string // Device fingerprint/identifier
}

// String returns a human-readable summary of the device.
func (d *DiscoveredDevice) String() string {
	if d.AmsNetId != "" {
		return fmt.Sprintf("Beckhoff TwinCAT at %s:%d (AMS: %s)", d.IP, d.Port, d.AmsNetId)
	}
	return fmt.Sprintf("Beckhoff TwinCAT at %s:%d", d.IP, d.Port)
}

// Discover scans a list of IP addresses for Beckhoff/TwinCAT devices by attempting
// to connect to TCP port 48898 and perform an ADS handshake.
//
// ips is a list of IP addresses to probe.
// timeout is the connection timeout per device (e.g., 500ms).
// concurrency is the number of parallel probes (e.g., 20).
//
// Returns discovered devices that responded to ADS protocol.
func Discover(ips []net.IP, timeout time.Duration, concurrency int) []DiscoveredDevice {
	if len(ips) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	if concurrency <= 0 {
		concurrency = 20
	}

	logging.DebugLog("tui", "ADS Discover: starting scan of %d IPs with concurrency=%d timeout=%v", len(ips), concurrency, timeout)

	var (
		results []DiscoveredDevice
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, concurrency)
		scanned int
	)

	for _, ip := range ips {
		wg.Add(1)
		sem <- struct{}{}

		go func(ip net.IP) {
			defer wg.Done()
			defer func() { <-sem }()

			device := probeADS(ip, timeout)
			if device != nil {
				mu.Lock()
				results = append(results, *device)
				mu.Unlock()
			}
			mu.Lock()
			scanned++
			if scanned%50 == 0 {
				logging.DebugLog("tui", "ADS Discover: scanned %d/%d IPs, found %d devices so far", scanned, len(ips), len(results))
			}
			mu.Unlock()
		}(ip)
	}

	wg.Wait()
	logging.DebugLog("tui", "ADS Discover: complete, scanned %d IPs, found %d devices", len(ips), len(results))
	return results
}

// DiscoverSubnet scans a subnet for Beckhoff/TwinCAT devices.
// cidr is in the format "192.168.1.0/24".
func DiscoverSubnet(cidr string, timeout time.Duration, concurrency int) ([]DiscoveredDevice, error) {
	logging.DebugLog("tui", "ADS DiscoverSubnet: expanding CIDR %s", cidr)
	ips, err := expandCIDR(cidr)
	if err != nil {
		logging.DebugLog("tui", "ADS DiscoverSubnet: expandCIDR error: %v", err)
		return nil, err
	}
	logging.DebugLog("tui", "ADS DiscoverSubnet: scanning %d IPs", len(ips))
	result := Discover(ips, timeout, concurrency)
	logging.DebugLog("tui", "ADS DiscoverSubnet: scan complete, found %d devices", len(result))
	return result, nil
}

// DiscoverBroadcast performs UDP broadcast discovery for TwinCAT devices.
// This discovers devices without requiring a route to be configured.
// broadcastAddr should be a broadcast address like "255.255.255.255" or "192.168.1.255".
func DiscoverBroadcast(broadcastAddrs []string, timeout time.Duration) []DiscoveredDevice {
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	logging.DebugLog("tui", "ADS DiscoverBroadcast: starting UDP discovery on %v", broadcastAddrs)

	var (
		results []DiscoveredDevice
		mu      sync.Mutex
		seen    = make(map[string]bool)
	)

	// Build the TwinCAT discovery request packets (try multiple formats)
	packets := buildDiscoveryPackets()

	for _, broadcastAddr := range broadcastAddrs {
		addr := fmt.Sprintf("%s:%d", broadcastAddr, DiscoveryUDPPort)

		conn, err := net.ListenPacket("udp4", ":0")
		if err != nil {
			logging.DebugLog("tui", "ADS DiscoverBroadcast: ListenPacket error: %v", err)
			continue
		}

		destAddr, err := net.ResolveUDPAddr("udp4", addr)
		if err != nil {
			conn.Close()
			logging.DebugLog("tui", "ADS DiscoverBroadcast: ResolveUDPAddr error: %v", err)
			continue
		}

		// Send all packet formats
		for i, packet := range packets {
			logging.DebugLog("tui", "ADS DiscoverBroadcast: sending format %d (%d bytes) to %s: %X",
				i, len(packet), addr, packet)
			_, err = conn.WriteTo(packet, destAddr)
			if err != nil {
				logging.DebugLog("tui", "ADS DiscoverBroadcast: WriteTo error: %v", err)
			}
		}

		// Read responses until timeout
		conn.SetReadDeadline(time.Now().Add(timeout))
		buf := make([]byte, 2048)

		for {
			n, srcAddr, err := conn.ReadFrom(buf)
			if err != nil {
				// Timeout or error - done with this broadcast
				logging.DebugLog("tui", "ADS DiscoverBroadcast: read complete on %s (timeout or error)", broadcastAddr)
				break
			}

			udpAddr, ok := srcAddr.(*net.UDPAddr)
			if !ok {
				continue
			}

			ipStr := udpAddr.IP.String()
			// Log first 64 bytes of response in hex for debugging
			hexLen := n
			if hexLen > 64 {
				hexLen = 64
			}
			logging.DebugLog("tui", "ADS DiscoverBroadcast: received %d bytes from %s: %X", n, ipStr, buf[:hexLen])

			// Parse the discovery response
			device := parseDiscoveryResponse(buf[:n], udpAddr.IP)
			if device != nil {
				mu.Lock()
				if !seen[ipStr] {
					seen[ipStr] = true
					results = append(results, *device)
					logging.DebugLog("tui", "ADS DiscoverBroadcast: found device: AmsNetId=%s Hostname=%s TwinCAT=%s",
						device.AmsNetId, device.Hostname, device.TwinCATVersion)
				}
				mu.Unlock()
			}
		}

		conn.Close()
	}

	logging.DebugLog("tui", "ADS DiscoverBroadcast: complete, found %d devices", len(results))
	return results
}

// buildDiscoveryPackets creates TwinCAT UDP discovery request packets.
// Returns multiple packet formats to try for compatibility.
func buildDiscoveryPackets() [][]byte {
	var packets [][]byte

	// Format 1: TwinCAT 3 discovery packet
	// Based on ADS router search protocol
	// Header: 03 66 14 71 (magic for discovery)
	packet1 := make([]byte, 32)
	binary.LittleEndian.PutUint32(packet1[0:4], 0x71146603) // Discovery magic
	binary.LittleEndian.PutUint32(packet1[4:8], 0x00000000) // Request ID
	binary.LittleEndian.PutUint32(packet1[8:12], 0x00000001) // Service: search
	packets = append(packets, packet1)

	// Format 2: Simple AMS broadcast request
	// Some TwinCAT versions respond to a simpler format
	packet2 := []byte{
		0x03, 0x66, 0x14, 0x71, // Magic
		0x00, 0x00, 0x00, 0x00, // Padding
		0x00, 0x00, 0x00, 0x00, // Padding
		0x00, 0x00, 0x00, 0x00, // Padding
	}
	packets = append(packets, packet2)

	// Format 3: TwinCAT System Service discovery
	// Port 10000 is used for system service, but 48899 should also respond
	packet3 := make([]byte, 8)
	binary.LittleEndian.PutUint32(packet3[0:4], 0x00000001) // Service ID
	binary.LittleEndian.PutUint32(packet3[4:8], 0x00000000) // Flags
	packets = append(packets, packet3)

	return packets
}

// buildDiscoveryPacket creates a TwinCAT UDP discovery request packet (legacy).
func buildDiscoveryPacket() []byte {
	packets := buildDiscoveryPackets()
	if len(packets) > 0 {
		return packets[0]
	}
	return nil
}

// parseDiscoveryResponse parses a TwinCAT UDP discovery response.
func parseDiscoveryResponse(data []byte, sourceIP net.IP) *DiscoveredDevice {
	if len(data) < 24 {
		logging.DebugLog("tui", "ADS parseDiscoveryResponse: packet too short (%d bytes)", len(data))
		return nil
	}

	// Check for discovery response magic
	magic := binary.LittleEndian.Uint32(data[0:4])
	logging.DebugLog("tui", "ADS parseDiscoveryResponse: magic=0x%08X", magic)

	// TwinCAT discovery responses have various formats
	// Try to extract what we can

	device := &DiscoveredDevice{
		IP:        sourceIP,
		Port:      DefaultTCPPort,
		Connected: true,
	}

	// Try to parse as standard TwinCAT discovery response
	// Response format varies by TwinCAT version

	// Look for AMS Net ID in the response (6 bytes, often at offset 4 or after header)
	// The exact offset depends on the response format

	// Try common offsets for AMS Net ID
	amsNetIdOffsets := []int{4, 8, 12}
	for _, offset := range amsNetIdOffsets {
		if offset+6 <= len(data) {
			// Check if this looks like a valid AMS Net ID (non-zero, reasonable values)
			b := data[offset : offset+6]
			if isValidAmsNetIdBytes(b) {
				device.AmsNetId = fmt.Sprintf("%d.%d.%d.%d.%d.%d", b[0], b[1], b[2], b[3], b[4], b[5])
				logging.DebugLog("tui", "ADS parseDiscoveryResponse: found AmsNetId at offset %d: %s", offset, device.AmsNetId)
				break
			}
		}
	}

	// If no AMS Net ID found from parsing, leave empty
	// (Don't derive from IP - it's often wrong)

	// Try to extract hostname (usually null-terminated string somewhere in the response)
	device.Hostname = extractNullString(data, 16, 64)
	if device.Hostname == "" {
		device.Hostname = extractNullString(data, 24, 64)
	}

	// Try to extract version info
	// TwinCAT version is often at a specific offset
	if len(data) >= 20 {
		// Look for version info patterns
		for offset := 10; offset < len(data)-4 && offset < 100; offset++ {
			// Check for version pattern (major.minor.build where major is 2 or 3)
			if data[offset] >= 2 && data[offset] <= 3 {
				major := data[offset]
				if offset+4 <= len(data) {
					minor := data[offset+1]
					build := binary.LittleEndian.Uint16(data[offset+2 : offset+4])
					if minor < 50 && build > 1000 && build < 65000 {
						device.TwinCATVersion = fmt.Sprintf("%d.%d.%d", major, minor, build)
						logging.DebugLog("tui", "ADS parseDiscoveryResponse: found version at offset %d: %s", offset, device.TwinCATVersion)
						break
					}
				}
			}
		}
	}

	// Build product name from available info
	if device.Hostname != "" {
		device.ProductName = fmt.Sprintf("TwinCAT on %s", device.Hostname)
	} else if device.TwinCATVersion != "" {
		device.ProductName = fmt.Sprintf("TwinCAT %s", device.TwinCATVersion)
	} else {
		device.ProductName = "Beckhoff TwinCAT"
	}

	// Try to extract OS info from later in the packet
	if len(data) > 80 {
		device.OSVersion = extractNullString(data, 64, 32)
	}

	return device
}

// isValidAmsNetIdBytes checks if 6 bytes look like a valid AMS Net ID.
func isValidAmsNetIdBytes(b []byte) bool {
	if len(b) != 6 {
		return false
	}
	// AMS Net ID should not be all zeros
	allZero := true
	for _, v := range b {
		if v != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return false
	}
	// Last two bytes are usually 1.1 for local devices
	// But can vary, so just check they're not wildly different
	// The first 4 bytes often match the IP address
	return true
}

// extractNullString extracts a null-terminated string from data.
func extractNullString(data []byte, offset, maxLen int) string {
	if offset >= len(data) {
		return ""
	}
	end := offset + maxLen
	if end > len(data) {
		end = len(data)
	}
	var result []byte
	for i := offset; i < end; i++ {
		if data[i] == 0 {
			break
		}
		// Only include printable ASCII
		if data[i] >= 32 && data[i] < 127 {
			result = append(result, data[i])
		}
	}
	return string(result)
}

// probeADS attempts to connect to a Beckhoff device and identify it.
func probeADS(ip net.IP, timeout time.Duration) *DiscoveredDevice {
	addr := fmt.Sprintf("%s:%d", ip.String(), DefaultTCPPort)

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil
	}
	defer conn.Close()

	logging.DebugLog("tui", "ADS probeADS: connected to %s, trying device info", addr)

	conn.SetDeadline(time.Now().Add(timeout))

	// Build a minimal ADS ReadDeviceInfo request
	// This is the simplest way to verify it's an ADS device
	device := tryADSDeviceInfo(conn, ip)
	if device != nil {
		logging.DebugLog("tui", "ADS probeADS: %s returned device: ProductName=%q AmsNetId=%s",
			addr, device.ProductName, device.AmsNetId)
		return device
	}

	logging.DebugLog("tui", "ADS probeADS: %s connected but no valid response (no route?)", addr)
	// If ReadDeviceInfo failed, but we connected, it's an ADS device without a route
	// The device accepted TCP connection on 48898 but didn't respond to ADS (no route configured)
	// AMS Net ID left empty - need proper discovery or manual configuration
	return &DiscoveredDevice{
		IP:          ip,
		Port:        DefaultTCPPort,
		AmsNetId:    "", // Unknown - no route means we can't query it
		ProductName: "Beckhoff TwinCAT (no route)",
		Connected:   false,
		HasRoute:    false, // No route - device didn't respond to ADS request
	}
}

// tryADSDeviceInfo attempts to read device info via ADS.
func tryADSDeviceInfo(conn net.Conn, ip net.IP) *DiscoveredDevice {
	logging.DebugLog("tui", "ADS tryADSDeviceInfo: starting for IP %s", ip.String())

	// Build source AMS Net ID from IP (common convention: ip.ip.ip.ip.1.1)
	var sourceNetId [6]byte
	if len(ip) == 4 {
		sourceNetId = [6]byte{ip[0], ip[1], ip[2], ip[3], 1, 1}
	} else if len(ip) >= 16 {
		sourceNetId = [6]byte{ip[12], ip[13], ip[14], ip[15], 1, 1}
	} else {
		logging.DebugLog("tui", "ADS tryADSDeviceInfo: unexpected IP length %d", len(ip))
		return nil
	}

	// Target AMS Net ID - try ip.ip.ip.ip.1.1 (common TwinCAT default)
	targetNetId := sourceNetId

	// Build AMS/TCP header (6 bytes)
	// Build AMS header (32 bytes)
	// Build ADS ReadDeviceInfo request (0 bytes data)

	amsDataLen := uint32(0) // ReadDeviceInfo has no data
	amsHeaderLen := uint32(32)
	tcpLen := amsHeaderLen + amsDataLen

	packet := make([]byte, 6+32+amsDataLen)

	// TCP Header
	binary.LittleEndian.PutUint16(packet[0:2], 0)          // Reserved
	binary.LittleEndian.PutUint32(packet[2:6], tcpLen)    // Length

	// AMS Header
	copy(packet[6:12], targetNetId[:])                    // Target Net ID
	binary.LittleEndian.PutUint16(packet[12:14], PortTC3PLC1) // Target Port (851 for TC3)
	copy(packet[14:20], sourceNetId[:])                   // Source Net ID
	binary.LittleEndian.PutUint16(packet[20:22], 32768)   // Source Port (arbitrary)
	binary.LittleEndian.PutUint16(packet[22:24], CmdReadDeviceInfo) // Command
	binary.LittleEndian.PutUint16(packet[24:26], StateFlagRequest) // State flags
	binary.LittleEndian.PutUint32(packet[26:30], amsDataLen)       // Data length
	binary.LittleEndian.PutUint32(packet[30:34], 0)       // Error code
	binary.LittleEndian.PutUint32(packet[34:38], 1)       // Invoke ID

	logging.DebugLog("tui", "ADS tryADSDeviceInfo: sending request packet")
	if _, err := conn.Write(packet); err != nil {
		logging.DebugLog("tui", "ADS tryADSDeviceInfo: write error: %v", err)
		return nil
	}

	logging.DebugLog("tui", "ADS tryADSDeviceInfo: reading response header")
	// Read response
	respHeader := make([]byte, 6)
	if _, err := conn.Read(respHeader); err != nil {
		logging.DebugLog("tui", "ADS tryADSDeviceInfo: read header error: %v", err)
		return nil
	}

	// Parse TCP header
	respLen := binary.LittleEndian.Uint32(respHeader[2:6])
	logging.DebugLog("tui", "ADS tryADSDeviceInfo: response length=%d", respLen)
	if respLen < 32 || respLen > 1024 {
		logging.DebugLog("tui", "ADS tryADSDeviceInfo: invalid response length")
		return nil
	}

	logging.DebugLog("tui", "ADS tryADSDeviceInfo: reading response data")
	respData := make([]byte, respLen)
	if _, err := conn.Read(respData); err != nil {
		logging.DebugLog("tui", "ADS tryADSDeviceInfo: read data error: %v", err)
		return nil
	}

	logging.DebugLog("tui", "ADS tryADSDeviceInfo: parsing response")
	// Check response command (should be ReadDeviceInfo response)
	if len(respData) < 32 {
		logging.DebugLog("tui", "ADS tryADSDeviceInfo: response too short")
		return nil
	}

	cmdId := binary.LittleEndian.Uint16(respData[16:18])
	logging.DebugLog("tui", "ADS tryADSDeviceInfo: cmdId=%d", cmdId)
	if cmdId != CmdReadDeviceInfo {
		logging.DebugLog("tui", "ADS tryADSDeviceInfo: wrong command id")
		return nil
	}

	stateFlags := binary.LittleEndian.Uint16(respData[18:20])
	logging.DebugLog("tui", "ADS tryADSDeviceInfo: stateFlags=%d", stateFlags)
	if stateFlags&0x0001 == 0 {
		// Not a response
		logging.DebugLog("tui", "ADS tryADSDeviceInfo: not a response")
		return nil
	}

	errorCode := binary.LittleEndian.Uint32(respData[24:28])
	logging.DebugLog("tui", "ADS tryADSDeviceInfo: errorCode=%d", errorCode)
	if errorCode != 0 {
		// ADS error, but device is ADS-capable and has route (it responded!)
		return &DiscoveredDevice{
			IP:          ip,
			Port:        DefaultTCPPort,
			AmsNetId:    fmt.Sprintf("%d.%d.%d.%d.%d.%d", targetNetId[0], targetNetId[1], targetNetId[2], targetNetId[3], targetNetId[4], targetNetId[5]),
			ProductName: "Beckhoff TwinCAT",
			Connected:   true,
			HasRoute:    true, // Device responded, so route is configured
		}
	}

	// Parse device info response
	// Response data: MajorVersion(1) MinorVersion(1) BuildVersion(2) DeviceName(16)
	if len(respData) < 32+24 {
		return &DiscoveredDevice{
			IP:          ip,
			Port:        DefaultTCPPort,
			AmsNetId:    fmt.Sprintf("%d.%d.%d.%d.%d.%d", targetNetId[0], targetNetId[1], targetNetId[2], targetNetId[3], targetNetId[4], targetNetId[5]),
			ProductName: "Beckhoff TwinCAT",
			Connected:   true,
			HasRoute:    true, // Device responded, so route is configured
		}
	}

	deviceData := respData[32:]
	majorVersion := deviceData[0]
	minorVersion := deviceData[1]
	buildVersion := binary.LittleEndian.Uint16(deviceData[2:4])

	logging.DebugLog("tui", "ADS tryADSDeviceInfo: version %d.%d.%d, parsing device name from bytes", majorVersion, minorVersion, buildVersion)

	// Device name is null-terminated string up to 16 bytes
	// Only include printable ASCII characters to avoid terminal corruption
	deviceName := ""
	for i := 4; i < 20 && i < len(deviceData); i++ {
		b := deviceData[i]
		if b == 0 {
			break
		}
		// Only include printable ASCII (32-126)
		if b >= 32 && b <= 126 {
			deviceName += string(b)
		}
	}

	logging.DebugLog("tui", "ADS tryADSDeviceInfo: parsed device name: %q", deviceName)

	productName := fmt.Sprintf("%s v%d.%d.%d", deviceName, majorVersion, minorVersion, buildVersion)
	if deviceName == "" {
		productName = fmt.Sprintf("TwinCAT v%d.%d.%d", majorVersion, minorVersion, buildVersion)
	}

	return &DiscoveredDevice{
		IP:             ip,
		Port:           DefaultTCPPort,
		AmsNetId:       fmt.Sprintf("%d.%d.%d.%d.%d.%d", targetNetId[0], targetNetId[1], targetNetId[2], targetNetId[3], targetNetId[4], targetNetId[5]),
		ProductName:    productName,
		Connected:      true,
		HasRoute:       true, // Device responded with device info, route is configured
		TwinCATVersion: fmt.Sprintf("%d.%d.%d", majorVersion, minorVersion, buildVersion),
	}
}

// expandCIDR expands a CIDR notation to a list of IP addresses.
func expandCIDR(cidr string) ([]net.IP, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("invalid CIDR: %w", err)
	}

	var ips []net.IP
	for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); inc(ip) {
		// Skip network and broadcast addresses for /24 and larger
		ones, bits := ipnet.Mask.Size()
		if bits-ones >= 8 {
			if ip[len(ip)-1] == 0 || ip[len(ip)-1] == 255 {
				continue
			}
		}
		ipCopy := make(net.IP, len(ip))
		copy(ipCopy, ip)
		ips = append(ips, ipCopy)
	}

	return ips, nil
}

// inc increments an IP address.
func inc(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
