// Package omron provides unified Omron PLC communication.
// This file implements CIP-based tag discovery for EIP (NJ/NX series).
package omron

import (
	"encoding/binary"
	"fmt"

	"warlogix/cip"
	"warlogix/logging"
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
	logging.DebugLog("EIP/Discovery", "Starting tag discovery using Get Instance Attribute List (0x55)")

	var allTags []TagInfo
	startInstance := uint32(0)

	// Pagination loop - limit to prevent infinite loops
	for page := 0; page < 1000; page++ {
		logging.DebugLog("EIP/Discovery", "Fetching page %d, starting at instance %d", page, startInstance)

		tags, lastInstance, hasMore, err := c.listSymbolsPage(startInstance)
		if err != nil {
			logging.DebugLog("EIP/Discovery", "Page %d error: %v", page, err)
			// If we got some tags before the error, return what we have
			if len(allTags) > 0 {
				logging.DebugLog("EIP/Discovery", "Returning %d tags collected before error", len(allTags))
				return allTags, nil
			}
			return nil, err
		}

		logging.DebugLog("EIP/Discovery", "Page %d: got %d tags, lastInstance=%d, hasMore=%v",
			page, len(tags), lastInstance, hasMore)

		allTags = append(allTags, tags...)

		if !hasMore || len(tags) == 0 {
			logging.DebugLog("EIP/Discovery", "Pagination complete after page %d", page)
			break
		}

		// Next page starts after the last instance we received
		startInstance = lastInstance + 1
	}

	logging.DebugLog("EIP/Discovery", "Discovery complete: %d total tags", len(allTags))
	return allTags, nil
}

// listSymbolsPage fetches one page of symbols using Get Instance Attribute List.
// This is much more efficient than iterating instance-by-instance.
// Note: This service (0x55) may not be supported by all Omron PLCs.
// The response format is based on Allen-Bradley Logix implementation.
func (c *Client) listSymbolsPage(startInstance uint32) (tags []TagInfo, lastInstance uint32, hasMore bool, err error) {
	// Build the path: Symbol Object class (0x6B) with starting instance
	path := c.buildSymbolPath(startInstance)

	logging.DebugLog("EIP/Discovery", "listSymbolsPage: startInstance=%d, path=%X", startInstance, path)

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

	logging.DebugLog("EIP/Discovery", "Request: service=0x55 path=%X attrs=[1,2,8]", path)

	// Send request
	cipReq := cip.Request{
		Service: svcGetInstanceAttributeList,
		Path:    path,
		Data:    attrData,
	}

	respData, err := c.sendCIPRequest(cipReq)
	if err != nil {
		logging.DebugLog("EIP/Discovery", "CIP request failed: %v", err)
		return nil, 0, false, fmt.Errorf("listSymbolsPage: %w", err)
	}

	logging.DebugLog("EIP/Discovery", "Response: %d bytes", len(respData))
	if len(respData) <= 64 {
		logging.DebugLog("EIP/Discovery", "Response data: %X", respData)
	} else {
		logging.DebugLog("EIP/Discovery", "Response data (first 64 bytes): %X...", respData[:64])
	}

	// Parse response header
	if len(respData) < 4 {
		logging.DebugLog("EIP/Discovery", "Response too short: %d bytes (need 4 minimum)", len(respData))
		return nil, 0, false, fmt.Errorf("response too short: %d bytes", len(respData))
	}

	replyService := respData[0]
	reserved := respData[1]
	status := respData[2]
	addlStatusSize := respData[3]

	logging.DebugLog("EIP/Discovery", "Response header: service=0x%02X reserved=0x%02X status=0x%02X addlStatusSize=%d",
		replyService, reserved, status, addlStatusSize)

	// Verify service reply
	if replyService != (svcGetInstanceAttributeList | 0x80) {
		logging.DebugLog("EIP/Discovery", "Unexpected reply service: 0x%02X (expected 0x%02X)",
			replyService, svcGetInstanceAttributeList|0x80)
		return nil, 0, false, fmt.Errorf("unexpected reply service: 0x%02X (expected 0x%02X)",
			replyService, svcGetInstanceAttributeList|0x80)
	}

	// Check for "partial transfer" (more data available)
	hasMore = (status == statusPartialTransfer)

	// Check for errors (but partial transfer is OK)
	if status != statusSuccess && status != statusPartialTransfer {
		statusMsg := "unknown"
		switch status {
		case 0x05:
			statusMsg = "path destination unknown"
		case 0x08:
			statusMsg = "service not supported"
		case 0x14:
			statusMsg = "attribute not supported"
		case 0x16:
			statusMsg = "object does not exist"
		}
		logging.DebugLog("EIP/Discovery", "CIP error: status=0x%02X (%s)", status, statusMsg)
		return nil, 0, false, fmt.Errorf("CIP error: status 0x%02X (%s)", status, statusMsg)
	}

	// Parse tag data (skip 4-byte header + additional status)
	dataStart := 4 + int(addlStatusSize)*2
	if dataStart >= len(respData) {
		logging.DebugLog("EIP/Discovery", "No tag data in response (dataStart=%d >= len=%d)", dataStart, len(respData))
		return nil, 0, hasMore, nil // No data
	}

	tagData := respData[dataStart:]
	logging.DebugLog("EIP/Discovery", "Parsing tag data: %d bytes starting at offset %d", len(tagData), dataStart)

	tags, lastInstance = c.parseSymbolListResponse(tagData)
	logging.DebugLog("EIP/Discovery", "Parsed %d tags, lastInstance=%d", len(tags), lastInstance)

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
//
// NOTE: This format is based on Allen-Bradley Logix. Omron NJ/NX PLCs may use
// a different format. If parsing fails, check the raw response bytes in the debug log.
func (c *Client) parseSymbolListResponse(data []byte) (tags []TagInfo, lastInstance uint32) {
	logging.DebugLog("EIP/Discovery", "parseSymbolListResponse: parsing %d bytes", len(data))

	i := 0
	entryNum := 0

	for i < len(data) {
		// Need at least 8 bytes to read the header (instance + unknown + nameLen)
		if i+8 > len(data) {
			logging.DebugLog("EIP/Discovery", "Entry %d at offset %d: insufficient header bytes (%d remaining)",
				entryNum, i, len(data)-i)
			break
		}

		// Instance ID at offset 0 (UINT - 2 bytes, used for pagination)
		instance := uint32(binary.LittleEndian.Uint16(data[i : i+2]))

		// Unknown field at offset 2
		unknown := binary.LittleEndian.Uint16(data[i+2 : i+4])

		// Name length at offset 4 (UINT - 2 bytes)
		nameLen := int(binary.LittleEndian.Uint16(data[i+4 : i+6]))

		logging.DebugLog("EIP/Discovery", "Entry %d at offset %d: instance=%d unknown=0x%04X nameLen=%d",
			entryNum, i, instance, unknown, nameLen)

		// Sanity check on name length
		if nameLen > 256 || nameLen < 0 {
			logging.DebugLog("EIP/Discovery", "Entry %d: invalid nameLen=%d, stopping parse", entryNum, nameLen)
			logging.DebugLog("EIP/Discovery", "Raw data at offset %d: %X", i, data[i:min(i+32, len(data))])
			break
		}

		// Each entry is nameLen + 20 bytes total (per Logix format)
		entrySize := nameLen + 20
		if i+entrySize > len(data) {
			logging.DebugLog("EIP/Discovery", "Entry %d: entrySize=%d exceeds remaining data (%d bytes)",
				entryNum, entrySize, len(data)-i)
			logging.DebugLog("EIP/Discovery", "Raw data at offset %d: %X", i, data[i:])
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

		logging.DebugLog("EIP/Discovery", "Entry %d: name=%q type=0x%04X (%s) arraySize=%d",
			entryNum, name, typeCode, TypeName(typeCode), arraySize)

		// Move to next entry
		i += entrySize
		entryNum++

		// Skip if this looks like a partial/invalid entry
		if name == "" || instance == 0 {
			logging.DebugLog("EIP/Discovery", "Skipping entry: empty name or zero instance")
			continue
		}

		// Skip system tags (internal tags starting with underscore or special prefixes)
		if len(name) > 0 && (name[0] == '_' || name[0] == '$') {
			logging.DebugLog("EIP/Discovery", "Skipping system tag: %s", name)
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

	logging.DebugLog("EIP/Discovery", "parseSymbolListResponse complete: %d tags, lastInstance=%d",
		len(tags), lastInstance)
	return tags, lastInstance
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// allTagsEIP discovers all tags using efficient CIP pagination.
// This replaces the old instance-by-instance approach.
func (c *Client) allTagsEIP() ([]TagInfo, error) {
	logging.DebugLog("EIP/Discovery", "allTagsEIP: using efficient pagination method")
	return c.listSymbols()
}

// allTagsEIPFallback is the legacy instance-by-instance discovery.
// Used as a fallback if the efficient method fails.
// This method uses Get Attributes All (0x01) on each instance.
func (c *Client) allTagsEIPFallback() ([]TagInfo, error) {
	logging.DebugLog("EIP/Discovery", "allTagsEIPFallback: using legacy instance-by-instance method")

	var tags []TagInfo
	instanceID := uint32(0)
	consecutiveErrors := 0

	for {
		instanceID++

		path, _ := cip.EPath().Class16(0x6B).Instance32(instanceID).Build()
		req := cip.Request{
			Service: svcGetAttributesAll,
			Path:    path,
		}

		if instanceID <= 10 || instanceID%100 == 0 {
			logging.DebugLog("EIP/Discovery", "Fallback: reading instance %d, path=%X", instanceID, path)
		}

		respData, err := c.sendCIPRequest(req)
		if err != nil {
			consecutiveErrors++
			if instanceID <= 10 {
				logging.DebugLog("EIP/Discovery", "Fallback instance %d error: %v", instanceID, err)
			}
			// Stop after 10 consecutive errors (end of symbol table)
			if consecutiveErrors >= 10 {
				logging.DebugLog("EIP/Discovery", "Fallback: stopping after %d consecutive errors", consecutiveErrors)
				break
			}
			continue
		}

		consecutiveErrors = 0

		if len(respData) < 4 {
			logging.DebugLog("EIP/Discovery", "Fallback instance %d: response too short (%d bytes)", instanceID, len(respData))
			break
		}

		tag := c.parseSymbolInstance(respData, instanceID)
		if tag.Name != "" {
			tags = append(tags, tag)
			if len(tags) <= 10 {
				logging.DebugLog("EIP/Discovery", "Fallback: found tag %q (type=0x%04X)", tag.Name, tag.TypeCode)
			}
		}

		if instanceID > 10000 {
			logging.DebugLog("EIP/Discovery", "Fallback: reached instance limit (10000)")
			break
		}
	}

	logging.DebugLog("EIP/Discovery", "Fallback complete: found %d tags", len(tags))
	return tags, nil
}
