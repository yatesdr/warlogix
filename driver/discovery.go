package driver

import (
	"fmt"
	"net"
	"sync"
	"time"

	"warlogix/ads"
	"warlogix/config"
	"warlogix/eip"
	"warlogix/logging"
	"warlogix/omron"
	"warlogix/s7"
)

// DiscoveredDevice represents a PLC discovered on the network.
type DiscoveredDevice struct {
	IP          net.IP            // Device IP address
	Port        uint16            // Protocol port
	Family      config.PLCFamily  // PLC family (logix, s7, beckhoff, omron)
	ProductName string            // Product name or description
	Protocol    string            // Protocol used for discovery
	Vendor      string            // Vendor name
	Extra       map[string]string // Additional info (serial, revision, etc.)
}

// String returns a human-readable summary of the device.
func (d *DiscoveredDevice) String() string {
	return fmt.Sprintf("%s %s at %s:%d (%s)", d.Vendor, d.ProductName, d.IP, d.Port, d.Protocol)
}

// DiscoverAll performs network discovery using all supported protocols.
// It broadcasts/probes for devices and returns all discovered PLCs.
//
// broadcastIP is used for EIP broadcast discovery (e.g., "255.255.255.255" or "192.168.1.255").
// scanCIDR is used for port-based scanning (e.g., "192.168.1.0/24"). If empty, uses local subnet.
// timeout is the timeout per device probe.
// concurrency is the number of parallel probes for scanning.
//
// Returns all discovered devices across all protocols.
func DiscoverAll(broadcastIP string, scanCIDR string, timeout time.Duration, concurrency int) []DiscoveredDevice {
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	if concurrency <= 0 {
		concurrency = 20
	}

	var (
		results []DiscoveredDevice
		mu      sync.Mutex
		wg      sync.WaitGroup
	)

	// Run all discoveries in parallel
	wg.Add(4)

	// 1. EIP broadcast discovery (Allen-Bradley, Omron NJ/NX)
	go func() {
		defer wg.Done()
		devices := discoverEIP(broadcastIP, timeout)
		mu.Lock()
		results = append(results, devices...)
		mu.Unlock()
	}()

	// 2. S7 port scan (Siemens)
	go func() {
		defer wg.Done()
		if scanCIDR == "" {
			return
		}
		devices := discoverS7(scanCIDR, timeout, concurrency)
		mu.Lock()
		results = append(results, devices...)
		mu.Unlock()
	}()

	// 3. ADS port scan (Beckhoff)
	go func() {
		defer wg.Done()
		if scanCIDR == "" {
			return
		}
		devices := discoverADS(scanCIDR, timeout, concurrency)
		mu.Lock()
		results = append(results, devices...)
		mu.Unlock()
	}()

	// 4. FINS discovery (Omron)
	go func() {
		defer wg.Done()
		if scanCIDR == "" {
			return
		}
		devices := discoverFINS(scanCIDR, timeout, concurrency)
		mu.Lock()
		results = append(results, devices...)
		mu.Unlock()
	}()

	wg.Wait()

	// Deduplicate by IP (prefer more specific protocol match)
	return deduplicateDevices(results)
}

// discoverEIP performs EIP broadcast discovery.
func discoverEIP(broadcastIP string, timeout time.Duration) []DiscoveredDevice {
	if broadcastIP == "" {
		broadcastIP = "255.255.255.255"
	}

	client := eip.NewEipClient("")
	identities, err := client.ListIdentityUDP(broadcastIP, timeout*3) // Give broadcast more time
	if err != nil {
		logging.DebugLog("Discovery", "EIP broadcast error: %v", err)
		return nil
	}

	var results []DiscoveredDevice
	for _, id := range identities {
		family := config.FamilyLogix
		vendor := "Rockwell Automation"

		// Check vendor ID for Omron
		if id.VendorID == 5 {
			family = config.FamilyOmron
			vendor = "Omron"
		}

		results = append(results, DiscoveredDevice{
			IP:          id.IP,
			Port:        id.Port,
			Family:      family,
			ProductName: id.ProductName,
			Protocol:    "EIP",
			Vendor:      vendor,
			Extra: map[string]string{
				"serial":   fmt.Sprintf("%d", id.SerialNumber),
				"revision": fmt.Sprintf("%d.%d", id.RevisionMajor, id.RevisionMinor),
				"vendorId": fmt.Sprintf("%d", id.VendorID),
			},
		})
	}

	logging.DebugLog("Discovery", "EIP found %d device(s)", len(results))
	return results
}

// discoverS7 scans for Siemens S7 PLCs.
func discoverS7(cidr string, timeout time.Duration, concurrency int) []DiscoveredDevice {
	devices, err := s7.DiscoverSubnet(cidr, timeout, concurrency)
	if err != nil {
		logging.DebugLog("Discovery", "S7 scan error: %v", err)
		return nil
	}

	var results []DiscoveredDevice
	for _, dev := range devices {
		results = append(results, DiscoveredDevice{
			IP:          dev.IP,
			Port:        dev.Port,
			Family:      config.FamilyS7,
			ProductName: dev.ProductName,
			Protocol:    "S7",
			Vendor:      "Siemens",
			Extra: map[string]string{
				"rack": fmt.Sprintf("%d", dev.Rack),
				"slot": fmt.Sprintf("%d", dev.Slot),
			},
		})
	}

	logging.DebugLog("Discovery", "S7 found %d device(s)", len(results))
	return results
}

// discoverADS scans for Beckhoff TwinCAT PLCs.
func discoverADS(cidr string, timeout time.Duration, concurrency int) []DiscoveredDevice {
	devices, err := ads.DiscoverSubnet(cidr, timeout, concurrency)
	if err != nil {
		logging.DebugLog("Discovery", "ADS scan error: %v", err)
		return nil
	}

	var results []DiscoveredDevice
	for _, dev := range devices {
		if !dev.Connected {
			continue // Skip unconfirmed devices
		}
		results = append(results, DiscoveredDevice{
			IP:          dev.IP,
			Port:        dev.Port,
			Family:      config.FamilyBeckhoff,
			ProductName: dev.ProductName,
			Protocol:    "ADS",
			Vendor:      "Beckhoff",
			Extra: map[string]string{
				"amsNetId": dev.AmsNetId,
			},
		})
	}

	logging.DebugLog("Discovery", "ADS found %d device(s)", len(results))
	return results
}

// discoverFINS scans for Omron FINS PLCs.
func discoverFINS(cidr string, timeout time.Duration, concurrency int) []DiscoveredDevice {
	devices, err := omron.NetworkDiscoverSubnet(cidr, timeout, concurrency)
	if err != nil {
		logging.DebugLog("Discovery", "FINS scan error: %v", err)
		return nil
	}

	var results []DiscoveredDevice
	for _, dev := range devices {
		results = append(results, DiscoveredDevice{
			IP:          dev.IP,
			Port:        dev.Port,
			Family:      config.FamilyOmron,
			ProductName: dev.ProductName,
			Protocol:    dev.Protocol,
			Vendor:      "Omron",
			Extra: map[string]string{
				"node": fmt.Sprintf("%d", dev.Node),
			},
		})
	}

	logging.DebugLog("Discovery", "FINS found %d device(s)", len(results))
	return results
}

// deduplicateDevices removes duplicate devices, preferring confirmed connections.
func deduplicateDevices(devices []DiscoveredDevice) []DiscoveredDevice {
	seen := make(map[string]int) // IP -> index in results
	var results []DiscoveredDevice

	for _, dev := range devices {
		key := dev.IP.String()
		if idx, ok := seen[key]; ok {
			// Prefer EIP over port scan, or update with more info
			existing := results[idx]
			if dev.Protocol == "EIP" && existing.Protocol != "EIP" {
				results[idx] = dev
			}
		} else {
			seen[key] = len(results)
			results = append(results, dev)
		}
	}

	return results
}

// GetLocalSubnets returns the CIDR notations for all local network interfaces.
func GetLocalSubnets() []string {
	var subnets []string

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	for _, iface := range ifaces {
		// Skip loopback and down interfaces
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			// Only IPv4
			if ipnet.IP.To4() == nil {
				continue
			}

			// Skip link-local
			if ipnet.IP.IsLinkLocalUnicast() {
				continue
			}

			subnets = append(subnets, ipnet.String())
		}
	}

	return subnets
}

// GetBroadcastAddresses returns broadcast addresses for all local interfaces.
func GetBroadcastAddresses() []string {
	var broadcasts []string

	ifaces, err := net.Interfaces()
	if err != nil {
		return []string{"255.255.255.255"}
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		if iface.Flags&net.FlagBroadcast == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipnet.IP.To4()
			if ip == nil {
				continue
			}

			if ip.IsLinkLocalUnicast() {
				continue
			}

			// Calculate broadcast address
			broadcast := make(net.IP, len(ip))
			for i := range ip {
				broadcast[i] = ip[i] | ^ipnet.Mask[i]
			}
			broadcasts = append(broadcasts, broadcast.String())
		}
	}

	if len(broadcasts) == 0 {
		return []string{"255.255.255.255"}
	}

	return broadcasts
}
