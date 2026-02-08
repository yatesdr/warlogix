package ads

import (
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"time"

	"warlogix/logging"
)

// DiscoveredDevice contains identity information about a discovered Beckhoff/TwinCAT device.
type DiscoveredDevice struct {
	IP          net.IP // Device IP address
	Port        uint16 // ADS port (48898)
	AmsNetId    string // AMS Net ID if discovered
	ProductName string // Product name if available
	Connected   bool   // True if successfully connected and identified
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

// probeADS attempts to connect to a Beckhoff device and identify it.
func probeADS(ip net.IP, timeout time.Duration) *DiscoveredDevice {
	addr := fmt.Sprintf("%s:%d", ip.String(), DefaultTCPPort)

	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(timeout))

	// Build a minimal ADS ReadDeviceInfo request
	// This is the simplest way to verify it's an ADS device
	device := tryADSDeviceInfo(conn, ip)
	if device != nil {
		return device
	}

	// If ReadDeviceInfo failed, but we connected, it might still be ADS
	// Just mark it as potentially ADS based on port response
	return &DiscoveredDevice{
		IP:          ip,
		Port:        DefaultTCPPort,
		ProductName: "Beckhoff TwinCAT (unconfirmed)",
		Connected:   false,
	}
}

// tryADSDeviceInfo attempts to read device info via ADS.
func tryADSDeviceInfo(conn net.Conn, ip net.IP) *DiscoveredDevice {
	// Build source AMS Net ID from IP (common convention: ip.ip.ip.ip.1.1)
	sourceNetId := [6]byte{ip[12], ip[13], ip[14], ip[15], 1, 1}
	if len(ip) == 4 {
		sourceNetId = [6]byte{ip[0], ip[1], ip[2], ip[3], 1, 1}
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

	if _, err := conn.Write(packet); err != nil {
		return nil
	}

	// Read response
	respHeader := make([]byte, 6)
	if _, err := conn.Read(respHeader); err != nil {
		return nil
	}

	// Parse TCP header
	respLen := binary.LittleEndian.Uint32(respHeader[2:6])
	if respLen < 32 || respLen > 1024 {
		return nil
	}

	respData := make([]byte, respLen)
	if _, err := conn.Read(respData); err != nil {
		return nil
	}

	// Check response command (should be ReadDeviceInfo response)
	if len(respData) < 32 {
		return nil
	}

	cmdId := binary.LittleEndian.Uint16(respData[16:18])
	if cmdId != CmdReadDeviceInfo {
		return nil
	}

	stateFlags := binary.LittleEndian.Uint16(respData[18:20])
	if stateFlags&0x0001 == 0 {
		// Not a response
		return nil
	}

	errorCode := binary.LittleEndian.Uint32(respData[24:28])
	if errorCode != 0 {
		// ADS error, but device is ADS-capable
		return &DiscoveredDevice{
			IP:          ip,
			Port:        DefaultTCPPort,
			AmsNetId:    fmt.Sprintf("%d.%d.%d.%d.%d.%d", targetNetId[0], targetNetId[1], targetNetId[2], targetNetId[3], targetNetId[4], targetNetId[5]),
			ProductName: "Beckhoff TwinCAT",
			Connected:   true,
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
		}
	}

	deviceData := respData[32:]
	majorVersion := deviceData[0]
	minorVersion := deviceData[1]
	buildVersion := binary.LittleEndian.Uint16(deviceData[2:4])

	// Device name is null-terminated string up to 16 bytes
	deviceName := ""
	for i := 4; i < 20 && i < len(deviceData); i++ {
		if deviceData[i] == 0 {
			break
		}
		deviceName += string(deviceData[i])
	}

	productName := fmt.Sprintf("%s v%d.%d.%d", deviceName, majorVersion, minorVersion, buildVersion)
	if deviceName == "" {
		productName = fmt.Sprintf("TwinCAT v%d.%d.%d", majorVersion, minorVersion, buildVersion)
	}

	return &DiscoveredDevice{
		IP:          ip,
		Port:        DefaultTCPPort,
		AmsNetId:    fmt.Sprintf("%d.%d.%d.%d.%d.%d", targetNetId[0], targetNetId[1], targetNetId[2], targetNetId[3], targetNetId[4], targetNetId[5]),
		ProductName: productName,
		Connected:   true,
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
