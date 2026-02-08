// Package omron batch operations for high-throughput reads.
// Implements batching strategies similar to Logix and S7 drivers.
package omron

import (
	"encoding/binary"
	"fmt"
	"sort"

	"warlogix/cip"
	"warlogix/eip"
	"warlogix/logging"
)

// Batch configuration constants
const (
	// FINS limits
	FINSMaxWordsPerRead   = 998  // Max words per single FINS read (protocol limit)
	FINSMaxMultiAreas     = 64   // Max areas in multi-memory read (model dependent, conservative)
	FINSMaxBitsPerRead    = 256  // Max bits per single FINS bit read

	// EIP/CIP limits
	EIPMaxServicesPerMSP  = 200  // CIP Multiple Service Packet limit
	EIPBatchSizeConnected = 50   // Batch size for connected messaging
	EIPBatchSizeUnconnected = 20 // Batch size for unconnected (conservative)
	EIPMaxResponsePayload = 4000 // Max response size for large Forward Open
)

// finsReadRequest represents a parsed FINS read request for batching.
type finsReadRequest struct {
	originalIndex int            // Position in original request list
	address       string         // Original address string
	parsed        *ParsedAddress // Parsed address components
	wordCount     int            // Number of words to read
}

// readGroup represents a group of contiguous reads that can be combined.
type readGroup struct {
	area      byte             // Memory area code
	startAddr uint16           // Starting address
	wordCount int              // Total words to read
	requests  []finsReadRequest // Requests in this group
}

// readEIPBatched reads multiple EIP/CIP tags using Multiple Service Packet batching.
func (c *Client) readEIPBatched(tagNames []string) ([]*TagValue, error) {
	if len(tagNames) == 0 {
		return nil, nil
	}

	results := make([]*TagValue, len(tagNames))

	// Determine batch size based on connection mode
	batchSize := EIPBatchSizeUnconnected
	if c.cipConn != nil {
		batchSize = EIPBatchSizeConnected
	}

	logging.DebugLog("Omron", "EIP batched read: %d tags, batch size=%d, connected=%v",
		len(tagNames), batchSize, c.cipConn != nil)

	// Process in batches
	for start := 0; start < len(tagNames); start += batchSize {
		end := start + batchSize
		if end > len(tagNames) {
			end = len(tagNames)
		}
		batch := tagNames[start:end]

		batchResults, err := c.readEIPBatch(batch)
		if err != nil {
			// If batch fails, fall back to individual reads for this batch
			logging.DebugLog("Omron", "EIP batch failed, falling back to individual: %v", err)
			for i, name := range batch {
				tv := c.readEIPSingle(name)
				results[start+i] = tv
			}
			continue
		}

		// Copy batch results to main results
		for i, tv := range batchResults {
			results[start+i] = tv
		}
	}

	return results, nil
}

// readEIPBatch reads a single batch of tags using Multiple Service Packet.
func (c *Client) readEIPBatch(tagNames []string) ([]*TagValue, error) {
	if len(tagNames) == 0 {
		return nil, nil
	}
	if len(tagNames) == 1 {
		// Single tag, no need for MSP overhead
		return []*TagValue{c.readEIPSingle(tagNames[0])}, nil
	}

	// Build individual read requests
	requests := make([]cip.MultiServiceRequest, len(tagNames))
	for i, tagName := range tagNames {
		path, err := cip.EPath().Symbol(tagName).Build()
		if err != nil {
			// Return error for invalid path
			return nil, fmt.Errorf("invalid tag path %q: %w", tagName, err)
		}

		requests[i] = cip.MultiServiceRequest{
			Service: svcReadTag,
			Path:    path,
			Data:    []byte{0x01, 0x00}, // Element count = 1
		}
	}

	// Build Multiple Service Packet
	msData, err := cip.BuildMultipleServiceRequest(requests)
	if err != nil {
		return nil, fmt.Errorf("failed to build MSP: %w", err)
	}

	// Build the complete CIP request with MSP service and path
	msPath, _ := cip.EPath().Class(0x02).Instance(1).Build() // Message Router
	reqData := make([]byte, 0, 2+len(msPath)+len(msData))
	reqData = append(reqData, cip.SvcMultipleServicePacket)
	reqData = append(reqData, msPath.WordLen())
	reqData = append(reqData, msPath...)
	reqData = append(reqData, msData...)

	// Send request
	cipResp, err := c.sendCIPRequestBatched(reqData)
	if err != nil {
		return nil, fmt.Errorf("MSP request failed: %w", err)
	}

	// Parse Multiple Service Packet response header
	if len(cipResp) < 4 {
		return nil, fmt.Errorf("MSP response too short")
	}

	replyService := cipResp[0]
	status := cipResp[2]
	addlStatusSize := cipResp[3]

	if replyService != (cip.SvcMultipleServicePacket | 0x80) {
		return nil, fmt.Errorf("unexpected reply service: 0x%02X", replyService)
	}

	// Status 0x1E = "Embedded service error" means MSP succeeded but some services failed
	if status != 0x00 && status != 0x1E {
		return nil, fmt.Errorf("MSP failed with status 0x%02X", status)
	}

	// Parse individual responses
	dataStart := 4 + int(addlStatusSize)*2
	responses, err := cip.ParseMultipleServiceResponse(cipResp[dataStart:])
	if err != nil {
		return nil, fmt.Errorf("failed to parse MSP response: %w", err)
	}

	if len(responses) != len(tagNames) {
		return nil, fmt.Errorf("expected %d responses, got %d", len(tagNames), len(responses))
	}

	// Convert responses to TagValues
	results := make([]*TagValue, len(tagNames))
	for i, resp := range responses {
		tv := &TagValue{
			Name:      tagNames[i],
			Count:     1,
			bigEndian: false, // CIP is little-endian
		}

		// Status 0x00 = success, 0x06 = partial transfer (OK for reads)
		if resp.Status != 0x00 && resp.Status != 0x06 {
			tv.Error = fmt.Errorf("CIP error 0x%02X", resp.Status)
			results[i] = tv
			continue
		}

		if len(resp.Data) < 2 {
			tv.Error = fmt.Errorf("response data too short")
			results[i] = tv
			continue
		}

		tv.DataType = binary.LittleEndian.Uint16(resp.Data[0:2])
		if len(resp.Data) > 2 {
			tv.Bytes = resp.Data[2:]
		}
		results[i] = tv
	}

	logging.DebugLog("Omron", "EIP batch read complete: %d tags in single request", len(tagNames))
	return results, nil
}

// sendCIPRequestBatched sends a CIP request, using connected messaging if available.
func (c *Client) sendCIPRequestBatched(reqData []byte) ([]byte, error) {
	if c.cipConn != nil {
		// Use connected messaging
		connData := c.cipConn.WrapConnected(reqData)
		cpf := c.buildConnectedCpf(connData)

		resp, err := c.eipClient.SendUnitDataTransaction(*cpf)
		if err != nil {
			return nil, err
		}

		if len(resp.Items) < 2 {
			return nil, fmt.Errorf("expected 2 CPF items")
		}

		_, cipResp, err := c.cipConn.UnwrapConnected(resp.Items[1].Data)
		return cipResp, err
	}

	// Use unconnected messaging
	cpf := eip.EipCommonPacket{
		Items: []eip.EipCommonPacketItem{
			{TypeId: eip.CpfAddressNullId, Length: 0, Data: nil},
			{TypeId: eip.CpfUnconnectedMessageId, Length: uint16(len(reqData)), Data: reqData},
		},
	}

	resp, err := c.eipClient.SendRRData(cpf)
	if err != nil {
		return nil, err
	}

	if len(resp.Items) < 2 {
		return nil, fmt.Errorf("invalid CIP response")
	}

	return resp.Items[1].Data, nil
}

// buildConnectedCpf builds a CPF packet for connected messaging.
func (c *Client) buildConnectedCpf(data []byte) *eip.EipCommonPacket {
	return &eip.EipCommonPacket{
		Items: []eip.EipCommonPacketItem{
			{
				TypeId: eip.CpfAddressConnectionId,
				Length: 4,
				Data:   binary.LittleEndian.AppendUint32(nil, c.cipConn.OTConnID),
			},
			{
				TypeId: eip.CpfConnectedTransportPacketId,
				Length: uint16(len(data)),
				Data:   data,
			},
		},
	}
}

// readEIPSingle reads a single EIP tag (fallback for batch failures).
func (c *Client) readEIPSingle(tagName string) *TagValue {
	tv := &TagValue{
		Name:      tagName,
		Count:     1,
		bigEndian: false,
	}

	path, err := cip.EPath().Symbol(tagName).Build()
	if err != nil {
		tv.Error = fmt.Errorf("invalid tag path: %w", err)
		return tv
	}

	reqData := binary.LittleEndian.AppendUint16(nil, 1)
	req := cip.Request{
		Service: svcReadTag,
		Path:    path,
		Data:    reqData,
	}

	respData, err := c.sendCIPRequest(req)
	if err != nil {
		tv.Error = err
		return tv
	}

	if len(respData) < 2 {
		tv.Error = fmt.Errorf("response too short")
		return tv
	}

	tv.DataType = binary.LittleEndian.Uint16(respData[0:2])
	if len(respData) > 2 {
		tv.Bytes = respData[2:]
	}
	return tv
}

// readFINSBatched reads multiple FINS addresses using optimized batching.
// Strategy:
// 1. Parse and classify all addresses
// 2. Group contiguous word addresses into bulk reads
// 3. Use multi-memory-read for scattered addresses
// 4. Read bits separately (grouped by word if possible)
func (c *Client) readFINSBatched(addresses []string) ([]*TagValue, error) {
	if len(addresses) == 0 {
		return nil, nil
	}

	results := make([]*TagValue, len(addresses))

	// Parse all addresses
	requests := make([]finsReadRequest, 0, len(addresses))
	var bitRequests []finsReadRequest

	for i, addr := range addresses {
		parsed, err := ParseAddress(addr)
		if err != nil {
			results[i] = &TagValue{Name: addr, Error: err, bigEndian: true}
			continue
		}

		wordCount := (TypeSize(parsed.TypeCode) * parsed.Count) / 2
		if wordCount < 1 {
			wordCount = 1
		}

		req := finsReadRequest{
			originalIndex: i,
			address:       addr,
			parsed:        parsed,
			wordCount:     wordCount,
		}

		if parsed.TypeCode == TypeBool {
			bitRequests = append(bitRequests, req)
		} else {
			requests = append(requests, req)
		}
	}

	// Process word requests with batching
	if len(requests) > 0 {
		c.readFINSWordsBatched(requests, results)
	}

	// Process bit requests
	for _, req := range bitRequests {
		c.readFINSBits(req, results)
	}

	return results, nil
}

// readFINSWordsBatched reads word requests with optimized batching.
func (c *Client) readFINSWordsBatched(requests []finsReadRequest, results []*TagValue) {
	if len(requests) == 0 {
		return
	}

	// Sort requests by area and address for grouping
	sort.Slice(requests, func(i, j int) bool {
		if requests[i].parsed.MemoryArea != requests[j].parsed.MemoryArea {
			return requests[i].parsed.MemoryArea < requests[j].parsed.MemoryArea
		}
		return requests[i].parsed.Address < requests[j].parsed.Address
	})

	// Group contiguous addresses
	groups := c.groupContiguousAddresses(requests)

	logging.DebugLog("Omron", "FINS batched read: %d requests grouped into %d groups",
		len(requests), len(groups))

	// Decide strategy based on number of groups
	if len(groups) == 1 || len(groups) <= 3 {
		// Few groups: use individual bulk reads
		for _, group := range groups {
			c.readFINSGroup(group, results)
		}
	} else if len(groups) <= FINSMaxMultiAreas {
		// Many non-contiguous groups: try multi-memory read
		err := c.readFINSMultiMemory(groups, results)
		if err != nil {
			// Fall back to individual group reads
			logging.DebugLog("Omron", "Multi-memory read failed, falling back: %v", err)
			for _, group := range groups {
				c.readFINSGroup(group, results)
			}
		}
	} else {
		// Too many groups for multi-memory, batch them
		for i := 0; i < len(groups); i += FINSMaxMultiAreas {
			end := i + FINSMaxMultiAreas
			if end > len(groups) {
				end = len(groups)
			}
			batch := groups[i:end]
			err := c.readFINSMultiMemory(batch, results)
			if err != nil {
				for _, group := range batch {
					c.readFINSGroup(group, results)
				}
			}
		}
	}
}

// groupContiguousAddresses groups requests with contiguous addresses.
func (c *Client) groupContiguousAddresses(requests []finsReadRequest) []readGroup {
	if len(requests) == 0 {
		return nil
	}

	var groups []readGroup
	currentGroup := readGroup{
		area:      requests[0].parsed.MemoryArea,
		startAddr: requests[0].parsed.Address,
		wordCount: requests[0].wordCount,
		requests:  []finsReadRequest{requests[0]},
	}

	for i := 1; i < len(requests); i++ {
		req := requests[i]

		// Check if this request is contiguous with current group
		expectedAddr := currentGroup.startAddr + uint16(currentGroup.wordCount)
		isContiguous := req.parsed.MemoryArea == currentGroup.area &&
			req.parsed.Address == expectedAddr &&
			currentGroup.wordCount+req.wordCount <= FINSMaxWordsPerRead

		if isContiguous {
			// Add to current group
			currentGroup.wordCount += req.wordCount
			currentGroup.requests = append(currentGroup.requests, req)
		} else {
			// Start new group
			groups = append(groups, currentGroup)
			currentGroup = readGroup{
				area:      req.parsed.MemoryArea,
				startAddr: req.parsed.Address,
				wordCount: req.wordCount,
				requests:  []finsReadRequest{req},
			}
		}
	}

	// Don't forget the last group
	groups = append(groups, currentGroup)

	return groups
}

// readFINSGroup reads a contiguous group of addresses in a single FINS request.
func (c *Client) readFINSGroup(group readGroup, results []*TagValue) {
	logging.DebugLog("Omron", "FINS group read: area=0x%02X addr=%d count=%d (%d tags)",
		group.area, group.startAddr, group.wordCount, len(group.requests))

	// Read all words in one request
	words, err := c.fins.readWords(group.area, group.startAddr, uint16(group.wordCount))
	if err != nil {
		// Mark all requests in group as failed
		for _, req := range group.requests {
			results[req.originalIndex] = &TagValue{
				Name:      req.address,
				Error:     err,
				bigEndian: true,
			}
		}
		return
	}

	// Distribute results to each request
	wordOffset := 0
	for _, req := range group.requests {
		tv := &TagValue{
			Name:      req.address,
			DataType:  req.parsed.TypeCode,
			Count:     req.parsed.Count,
			bigEndian: true,
		}

		// Extract this request's words
		data := make([]byte, req.wordCount*2)
		for j := 0; j < req.wordCount && wordOffset+j < len(words); j++ {
			binary.BigEndian.PutUint16(data[j*2:j*2+2], words[wordOffset+j])
		}
		tv.Bytes = data

		if req.parsed.Count > 1 {
			tv.DataType = MakeArrayType(req.parsed.TypeCode)
		}

		results[req.originalIndex] = tv
		wordOffset += req.wordCount
	}
}

// readFINSMultiMemory reads multiple non-contiguous areas using FINS 0x0104.
func (c *Client) readFINSMultiMemory(groups []readGroup, results []*TagValue) error {
	if len(groups) == 0 {
		return nil
	}

	logging.DebugLog("Omron", "FINS multi-memory read: %d groups", len(groups))

	// Build multi-memory read request
	data := BuildMultiMemoryReadRequest(groups)

	resp, err := c.fins.sendCommand(FINSCmdMultiMemoryRead, data)
	if err != nil {
		return err
	}

	// Parse response and distribute to results
	return ParseMultiMemoryReadResponse(resp, groups, results)
}

// readFINSBits reads bit requests (one at a time for now, could be optimized).
func (c *Client) readFINSBits(req finsReadRequest, results []*TagValue) {
	tv := &TagValue{
		Name:      req.address,
		DataType:  TypeBool,
		Count:     req.parsed.Count,
		bigEndian: true,
	}

	bitArea := BitAreaFromWordArea(req.parsed.MemoryArea)
	bits, err := c.fins.readBits(bitArea, req.parsed.Address, req.parsed.BitOffset, uint16(req.parsed.Count))
	if err != nil {
		tv.Error = err
		results[req.originalIndex] = tv
		return
	}

	// Convert bits to bytes
	data := make([]byte, len(bits)*2)
	for j, b := range bits {
		if b {
			data[j*2+1] = 1
		}
	}
	tv.Bytes = data

	if req.parsed.Count > 1 {
		tv.DataType = MakeArrayType(TypeBool)
	}

	results[req.originalIndex] = tv
}

// BuildMultiMemoryReadRequest builds a FINS 0x0104 multi-memory read request.
func BuildMultiMemoryReadRequest(groups []readGroup) []byte {
	// Format: [area_count] + [area, addr_hi, addr_lo, bit_offset, count_hi, count_lo] * n
	// Each area entry is 6 bytes
	data := make([]byte, 1+len(groups)*6)
	data[0] = byte(len(groups))

	for i, group := range groups {
		offset := 1 + i*6
		data[offset] = group.area
		data[offset+1] = byte(group.startAddr >> 8)
		data[offset+2] = byte(group.startAddr)
		data[offset+3] = 0 // bit offset
		binary.BigEndian.PutUint16(data[offset+4:offset+6], uint16(group.wordCount))
	}

	logging.DebugLog("Omron", "MultiMemoryRead request: %d areas, %d bytes", len(groups), len(data))
	return data
}

// ParseMultiMemoryReadResponse parses a FINS 0x0104 response.
func ParseMultiMemoryReadResponse(resp []byte, groups []readGroup, results []*TagValue) error {
	// Response format: [data for area 1] + [data for area 2] + ...
	// Each area's data is wordCount * 2 bytes

	offset := 0
	for _, group := range groups {
		expectedBytes := group.wordCount * 2
		if offset+expectedBytes > len(resp) {
			// Not enough data, mark remaining as errors
			for _, req := range group.requests {
				if results[req.originalIndex] == nil {
					results[req.originalIndex] = &TagValue{
						Name:      req.address,
						Error:     fmt.Errorf("multi-memory response too short"),
						bigEndian: true,
					}
				}
			}
			continue
		}

		// Extract words for this group
		groupData := resp[offset : offset+expectedBytes]
		offset += expectedBytes

		// Distribute to requests
		wordOffset := 0
		for _, req := range group.requests {
			tv := &TagValue{
				Name:      req.address,
				DataType:  req.parsed.TypeCode,
				Count:     req.parsed.Count,
				bigEndian: true,
			}

			reqBytes := req.wordCount * 2
			if wordOffset+reqBytes <= len(groupData) {
				tv.Bytes = make([]byte, reqBytes)
				copy(tv.Bytes, groupData[wordOffset:wordOffset+reqBytes])
			}

			if req.parsed.Count > 1 {
				tv.DataType = MakeArrayType(req.parsed.TypeCode)
			}

			results[req.originalIndex] = tv
			wordOffset += reqBytes
		}
	}

	return nil
}
