package driver

import (
	"context"
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
	DiscoveredAt time.Time        // When this device was discovered
}

// String returns a human-readable summary of the device.
func (d *DiscoveredDevice) String() string {
	return fmt.Sprintf("%s %s at %s:%d (%s)", d.Vendor, d.ProductName, d.IP, d.Port, d.Protocol)
}

// Key returns a unique identifier for deduplication.
func (d *DiscoveredDevice) Key() string {
	return fmt.Sprintf("%s:%d:%s", d.IP.String(), d.Port, d.Protocol)
}

// DiscoverySession manages an ongoing discovery process.
type DiscoverySession struct {
	ctx        context.Context
	cancel     context.CancelFunc
	devices    []DiscoveredDevice
	deviceChan chan DiscoveredDevice
	mu         sync.RWMutex
	seen       map[string]bool
	done       chan struct{}
}

// NewDiscoverySession creates a new discovery session.
func NewDiscoverySession() *DiscoverySession {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	return &DiscoverySession{
		ctx:        ctx,
		cancel:     cancel,
		deviceChan: make(chan DiscoveredDevice, 100),
		seen:       make(map[string]bool),
		done:       make(chan struct{}),
	}
}

// Start begins discovery in the background.
// Returns a channel that receives devices as they are discovered.
func (s *DiscoverySession) Start(broadcastIP string, scanCIDR string, concurrency int) <-chan DiscoveredDevice {
	if concurrency <= 0 {
		concurrency = 50
	}

	go func() {
		defer close(s.deviceChan)
		defer close(s.done)

		var wg sync.WaitGroup

		// 1. EIP broadcast discovery (runs continuously until context cancelled)
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.discoverEIPContinuous(broadcastIP)
		}()

		// 2. S7 port scan
		if scanCIDR != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.discoverS7(scanCIDR, concurrency)
			}()
		}

		// 3. ADS port scan
		if scanCIDR != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.discoverADS(scanCIDR, concurrency)
			}()
		}

		// 4. FINS discovery
		if scanCIDR != "" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.discoverFINS(scanCIDR, concurrency)
			}()
		}

		wg.Wait()
	}()

	return s.deviceChan
}

// Stop cancels the discovery session.
func (s *DiscoverySession) Stop() {
	s.cancel()
}

// Wait blocks until discovery is complete or cancelled.
func (s *DiscoverySession) Wait() {
	<-s.done
}

// Devices returns all discovered devices so far.
func (s *DiscoverySession) Devices() []DiscoveredDevice {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]DiscoveredDevice, len(s.devices))
	copy(result, s.devices)
	return result
}

// addDevice adds a device if not already seen.
func (s *DiscoverySession) addDevice(dev DiscoveredDevice) {
	key := dev.Key()
	s.mu.Lock()
	if s.seen[key] {
		s.mu.Unlock()
		return
	}
	s.seen[key] = true
	dev.DiscoveredAt = time.Now()
	s.devices = append(s.devices, dev)
	s.mu.Unlock()

	// Try to send to channel (non-blocking if full)
	select {
	case s.deviceChan <- dev:
	default:
	}
}

// discoverEIPContinuous performs repeated EIP broadcast discovery.
func (s *DiscoverySession) discoverEIPContinuous(broadcastIP string) {
	if broadcastIP == "" {
		broadcastIP = "255.255.255.255"
	}

	// Also try directed broadcasts
	broadcasts := []string{broadcastIP}
	for _, b := range GetBroadcastAddresses() {
		if b != broadcastIP {
			broadcasts = append(broadcasts, b)
		}
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Do immediate discovery first
	for _, bcast := range broadcasts {
		if s.ctx.Err() != nil {
			return
		}
		s.discoverEIPOnce(bcast, 1500*time.Millisecond)
	}

	// Continue periodic discovery
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			for _, bcast := range broadcasts {
				if s.ctx.Err() != nil {
					return
				}
				s.discoverEIPOnce(bcast, 1500*time.Millisecond)
			}
		}
	}
}

// discoverEIPOnce performs a single EIP broadcast discovery.
func (s *DiscoverySession) discoverEIPOnce(broadcastIP string, timeout time.Duration) {
	client := eip.NewEipClient("")
	identities, err := client.ListIdentityUDP(broadcastIP, timeout)
	if err != nil {
		logging.DebugLog("Discovery", "EIP broadcast to %s error: %v", broadcastIP, err)
		return
	}

	for _, id := range identities {
		family := config.FamilyLogix
		vendor := "Rockwell Automation"

		// Check vendor ID for Omron
		if id.VendorID == 5 {
			family = config.FamilyOmron
			vendor = "Omron"
		}

		s.addDevice(DiscoveredDevice{
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
}

// discoverS7 scans for Siemens S7 PLCs.
func (s *DiscoverySession) discoverS7(cidr string, concurrency int) {
	devices, err := s7.DiscoverSubnet(cidr, 500*time.Millisecond, concurrency)
	if err != nil {
		logging.DebugLog("Discovery", "S7 scan error: %v", err)
		return
	}

	for _, dev := range devices {
		if s.ctx.Err() != nil {
			return
		}
		s.addDevice(DiscoveredDevice{
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

	logging.DebugLog("Discovery", "S7 found %d device(s)", len(devices))
}

// discoverADS scans for Beckhoff TwinCAT PLCs.
func (s *DiscoverySession) discoverADS(cidr string, concurrency int) {
	devices, err := ads.DiscoverSubnet(cidr, 500*time.Millisecond, concurrency)
	if err != nil {
		logging.DebugLog("Discovery", "ADS scan error: %v", err)
		return
	}

	for _, dev := range devices {
		if s.ctx.Err() != nil {
			return
		}
		if !dev.Connected {
			continue
		}
		s.addDevice(DiscoveredDevice{
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

	logging.DebugLog("Discovery", "ADS found %d device(s)", len(devices))
}

// discoverFINS scans for Omron FINS PLCs.
func (s *DiscoverySession) discoverFINS(cidr string, concurrency int) {
	devices, err := omron.NetworkDiscoverSubnet(cidr, 500*time.Millisecond, concurrency)
	if err != nil {
		logging.DebugLog("Discovery", "FINS scan error: %v", err)
		return
	}

	for _, dev := range devices {
		if s.ctx.Err() != nil {
			return
		}
		s.addDevice(DiscoveredDevice{
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

	logging.DebugLog("Discovery", "FINS found %d device(s)", len(devices))
}

// DiscoverAll performs network discovery using all supported protocols.
// This is the synchronous version that waits for completion.
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
	identities, err := client.ListIdentityUDP(broadcastIP, timeout*3)
	if err != nil {
		logging.DebugLog("Discovery", "EIP broadcast error: %v", err)
		return nil
	}

	var results []DiscoveredDevice
	for _, id := range identities {
		family := config.FamilyLogix
		vendor := "Rockwell Automation"

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
			continue
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
	seen := make(map[string]int)
	var results []DiscoveredDevice

	for _, dev := range devices {
		key := dev.IP.String()
		if idx, ok := seen[key]; ok {
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

			if ipnet.IP.To4() == nil {
				continue
			}

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
