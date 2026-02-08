// Package omron provides unified Omron PLC communication.
// This file implements CIP-based tag discovery for EIP (NJ/NX series).
package omron

import (
	"encoding/binary"
	"fmt"

	"warlogix/cip"
)

// CIP service codes for symbol discovery.
const (
	svcGetAttributesAll         byte = 0x01 // Get Attributes All
	svcGetInstanceAttributeList byte = 0x55 // Get Instance Attribute List (efficient pagination)

	// Symbol Object class ID
	classSymbol byte = 0x6B
)

// CIP status codes.
const (
	statusSuccess         byte = 0x00 // Success
	statusPartialTransfer byte = 0x06 // More data available (pagination)
)

// listSymbols queries the Symbol Object (class 0x6B) for tag information.
// Uses pagination for efficient discovery of large tag databases.
// Returns all tags discovered from the PLC.
func (c *Client) listSymbols() ([]TagInfo, error) {
	var allTags []TagInfo
	startInstance := uint32(0)

	// Pagination loop - limit to prevent infinite loops
	for page := 0; page < 1000; page++ {
		tags, lastInstance, hasMore, err := c.listSymbolsPage(startInstance)
		if err != nil {
			// If we got some tags before the error, return what we have
			if len(allTags) > 0 {
				return allTags, nil
			}
			return nil, err
		}

		allTags = append(allTags, tags...)

		if !hasMore || len(tags) == 0 {
			break
		}

		// Next page starts after the last instance we received
		startInstance = lastInstance + 1
	}

	return allTags, nil
}

// listSymbolsPage fetches one page of symbols using Get Instance Attribute List.
// This is much more efficient than iterating instance-by-instance.
func (c *Client) listSymbolsPage(startInstance uint32) (tags []TagInfo, lastInstance uint32, hasMore bool, err error) {
	// Build the path: Symbol Object class (0x6B) with starting instance
	path := c.buildSymbolPath(startInstance)

	// Request attributes: name (1), type (2), byte count (8)
	// This matches the Logix pattern for getting array sizes during discovery
	attrData := []byte{
		0x03, 0x00, // Attribute count: 3
		0x01, 0x00, // Attribute 1: Symbol Name
		0x02, 0x00, // Attribute 2: Symbol Type
		0x08, 0x00, // Attribute 8: Byte Count (for array size calculation)
	}

	// Build the CIP request
	reqData := make([]byte, 0, 2+len(path)+len(attrData))
	reqData = append(reqData, svcGetInstanceAttributeList) // Service 0x55
	reqData = append(reqData, byte(len(path)/2))           // Path word length
	reqData = append(reqData, path...)
	reqData = append(reqData, attrData...)

	// Send request
	cipReq := cip.Request{
		Service: svcGetInstanceAttributeList,
		Path:    path,
		Data:    attrData,
	}

	respData, err := c.sendCIPRequest(cipReq)
	if err != nil {
		return nil, 0, false, fmt.Errorf("listSymbolsPage: %w", err)
	}

	// Parse response header
	if len(respData) < 4 {
		return nil, 0, false, fmt.Errorf("response too short: %d bytes", len(respData))
	}

	replyService := respData[0]
	status := respData[2]
	addlStatusSize := respData[3]

	// Verify service reply
	if replyService != (svcGetInstanceAttributeList | 0x80) {
		return nil, 0, false, fmt.Errorf("unexpected reply service: 0x%02X", replyService)
	}

	// Check for "partial transfer" (more data available)
	hasMore = (status == statusPartialTransfer)

	// Check for errors (but partial transfer is OK)
	if status != statusSuccess && status != statusPartialTransfer {
		return nil, 0, false, fmt.Errorf("CIP error: status 0x%02X", status)
	}

	// Parse tag data (skip 4-byte header + additional status)
	dataStart := 4 + int(addlStatusSize)*2
	if dataStart >= len(respData) {
		return nil, 0, hasMore, nil // No data
	}

	tags, lastInstance = c.parseSymbolListResponse(respData[dataStart:])
	return tags, lastInstance, hasMore, nil
}

// buildSymbolPath builds the EPath for symbol listing.
func (c *Client) buildSymbolPath(startInstance uint32) cip.EPath_t {
	builder := cip.EPath()

	// Add Symbol Object class (0x6B)
	builder = builder.Class(classSymbol)

	// Add instance (using appropriate size encoding)
	if startInstance <= 0xFF {
		builder = builder.Instance(byte(startInstance))
	} else if startInstance <= 0xFFFF {
		builder = builder.Instance16(uint16(startInstance))
	} else {
		builder = builder.Instance32(startInstance)
	}

	path, _ := builder.Build()
	return path
}

// parseSymbolListResponse parses the tag list data from Get Instance Attribute List response.
// Response format per tag (each entry is nameLen + 20 bytes):
// - Offset 0: Instance ID (2 bytes, UINT) - used for pagination
// - Offset 2: Unknown (2 bytes)
// - Offset 4: Name length (2 bytes, UINT)
// - Offset 6: Tag name (nameLen bytes)
// - Offset 6+nameLen: Symbol type (2 bytes, UINT)
// - Offset 8+nameLen: Array size (2 bytes, UINT) - element count for arrays
// - Remaining bytes up to nameLen+20: additional metadata
func (c *Client) parseSymbolListResponse(data []byte) (tags []TagInfo, lastInstance uint32) {
	i := 0

	for i < len(data) {
		// Need at least 8 bytes to read the header (instance + unknown + nameLen)
		if i+8 > len(data) {
			break
		}

		// Instance ID at offset 0 (UINT - 2 bytes, used for pagination)
		instance := uint32(binary.LittleEndian.Uint16(data[i : i+2]))

		// Name length at offset 4 (UINT - 2 bytes)
		nameLen := int(binary.LittleEndian.Uint16(data[i+4 : i+6]))

		// Each entry is nameLen + 20 bytes total
		entrySize := nameLen + 20
		if i+entrySize > len(data) {
			break
		}

		// Extract the entry
		entry := data[i : i+entrySize]

		// Tag name at offset 6
		name := string(entry[6 : 6+nameLen])

		// Symbol type at offset 6+nameLen (UINT - 2 bytes)
		typeCode := binary.LittleEndian.Uint16(entry[6+nameLen : 8+nameLen])

		// Array size at offset 8+nameLen (UINT - 2 bytes) - element count for arrays
		arraySize := binary.LittleEndian.Uint16(entry[8+nameLen : 10+nameLen])

		// Move to next entry
		i += entrySize

		// Skip if this looks like a partial/invalid entry
		if name == "" || instance == 0 {
			continue
		}

		// Skip system tags (internal tags starting with underscore or special prefixes)
		if len(name) > 0 && (name[0] == '_' || name[0] == '$') {
			continue
		}

		// Calculate dimensions from array size for array types
		var dimensions []uint32
		if IsArray(typeCode) && arraySize > 0 {
			dimensions = []uint32{uint32(arraySize)}
		}

		tags = append(tags, TagInfo{
			Name:       name,
			TypeCode:   typeCode,
			Instance:   instance,
			Dimensions: dimensions,
		})

		lastInstance = instance
	}

	return tags, lastInstance
}

// allTagsEIP discovers all tags using efficient CIP pagination.
// This replaces the old instance-by-instance approach.
func (c *Client) allTagsEIP() ([]TagInfo, error) {
	return c.listSymbols()
}

// allTagsEIPFallback is the legacy instance-by-instance discovery.
// Used as a fallback if the efficient method fails.
func (c *Client) allTagsEIPFallback() ([]TagInfo, error) {
	var tags []TagInfo
	instanceID := uint32(0)

	for {
		instanceID++

		path, _ := cip.EPath().Class16(0x6B).Instance32(instanceID).Build()
		req := cip.Request{
			Service: svcGetAttributesAll,
			Path:    path,
		}

		respData, err := c.sendCIPRequest(req)
		if err != nil {
			break
		}

		if len(respData) < 4 {
			break
		}

		tag := c.parseSymbolInstance(respData, instanceID)
		if tag.Name != "" {
			tags = append(tags, tag)
		}

		if instanceID > 10000 {
			break
		}
	}

	return tags, nil
}
