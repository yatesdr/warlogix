// Package omron provides unified Omron PLC communication.
// This file implements CIP-based tag discovery for EIP (NJ/NX series).
//
// Omron NJ/NX series PLCs use CIP (Common Industrial Protocol) over EtherNet/IP,
// but their implementation differs from Allen-Bradley Logix:
//
//   - Get Instance Attribute List (0x55) is NOT supported by Omron
//   - Must use Get Attributes All (0x01) on individual Symbol Object instances
//   - Class 0x6A contains symbol table metadata (instance count)
//   - Class 0x6B contains the actual symbol/tag instances
//   - String data uses odd-byte padding (single byte when name length is odd)
//   - System variables start with underscore ('_')
//
// References:
//   - libplctag GitHub issues #317, #466
//   - Omron NJ/NX-series CPU Unit Built-in EtherNet/IP Port User's Manual (W506)
package omron

import (
	"encoding/binary"
	"fmt"

	"warlink/cip"
	"warlink/logging"
)

// CIP service codes for symbol discovery.
const (
	svcGetAttributesAll byte = 0x01 // Get Attributes All - the only discovery method Omron supports

	// Symbol Object class IDs (Omron-specific)
	classSymbolTable byte = 0x6A // Symbol table metadata (contains instance count)
	classSymbol      byte = 0x6B // Symbol Object (tag instances)

	// Note: Omron does NOT support service 0x55 (Get Instance Attribute List)
	// which is commonly used for efficient pagination in Allen-Bradley Logix PLCs.
	// Attempting to use 0x55 returns CIP status 0x08 ("service not supported").
)

// CIP status codes.
const (
	statusSuccess             byte = 0x00 // Success
	statusPartialTransfer     byte = 0x06 // More data available (pagination - not used by Omron)
	statusPathDestUnknown     byte = 0x05 // Path destination unknown
	statusServiceNotSupported byte = 0x08 // Service not supported
	statusAttrNotSupported    byte = 0x14 // Attribute not supported
	statusObjectNotExists     byte = 0x16 // Object does not exist
)

// listSymbols queries the Symbol Object (class 0x6B) for tag information.
// Uses Omron-specific instance-by-instance iteration with Get Attributes All (0x01).
// Returns all tags discovered from the PLC.
//
// NOTE: Unlike Allen-Bradley Logix PLCs, Omron does NOT support Get Instance
// Attribute List (0x55) for efficient pagination. We must iterate through
// instances one at a time using Get Attributes All (0x01).
func (c *Client) listSymbols() ([]TagInfo, error) {
	logging.DebugLog("EIP/Discovery", "Starting Omron-specific tag discovery using Get Attributes All (0x01)")

	// First, try to get the symbol table metadata from class 0x6A
	// This tells us how many symbols exist
	maxInstance := c.getSymbolTableCount()
	if maxInstance > 0 {
		logging.DebugLog("EIP/Discovery", "Symbol table reports %d instances", maxInstance)
	} else {
		// If we can't get the count, use a reasonable default limit
		maxInstance = 10000
		logging.DebugLog("EIP/Discovery", "Symbol table metadata unavailable, using limit of %d", maxInstance)
	}

	var allTags []TagInfo
	consecutiveErrors := 0
	const maxConsecutiveErrors = 10 // Stop after 10 consecutive errors (end of table)

	// Iterate through instances starting at 1 (instance 0 is class-level)
	for instance := uint32(1); instance <= uint32(maxInstance); instance++ {
		tag, err := c.getSymbolInstance(instance)
		if err != nil {
			consecutiveErrors++
			if instance <= 10 {
				logging.DebugLog("EIP/Discovery", "Instance %d error: %v", instance, err)
			}

			// Stop after too many consecutive errors (indicates end of symbol table)
			if consecutiveErrors >= maxConsecutiveErrors {
				logging.DebugLog("EIP/Discovery", "Stopping after %d consecutive errors at instance %d",
					consecutiveErrors, instance)
				break
			}
			continue
		}

		consecutiveErrors = 0 // Reset on success

		// Skip empty/invalid tags
		if tag.Name == "" {
			continue
		}

		// Skip system tags (start with underscore or dollar sign)
		if len(tag.Name) > 0 && (tag.Name[0] == '_' || tag.Name[0] == '$') {
			if instance <= 20 {
				logging.DebugLog("EIP/Discovery", "Skipping system tag: %s", tag.Name)
			}
			continue
		}

		allTags = append(allTags, tag)

		// Log progress periodically
		if instance <= 10 || instance%100 == 0 {
			logging.DebugLog("EIP/Discovery", "Instance %d: found tag %q (type=0x%04X %s)",
				instance, tag.Name, tag.TypeCode, TypeName(tag.TypeCode))
		}
	}

	logging.DebugLog("EIP/Discovery", "Discovery complete: %d user tags found", len(allTags))
	return allTags, nil
}

// getSymbolTableCount queries class 0x6A to get the number of symbol instances.
// This is Omron-specific - the Symbol Table class contains metadata about
// the symbol table including the instance count.
// Returns 0 if the query fails (caller should use a default limit).
func (c *Client) getSymbolTableCount() uint32 {
	logging.DebugLog("EIP/Discovery", "Querying symbol table metadata (class 0x6A)")

	// Build path to Symbol Table class (0x6A), instance 0 (class-level)
	path, _ := cip.EPath().Class(classSymbolTable).Instance(0x00).Build()

	req := cip.Request{
		Service: svcGetAttributesAll,
		Path:    path,
	}

	respData, err := c.sendCIPRequest(req)
	if err != nil {
		logging.DebugLog("EIP/Discovery", "Symbol table query failed: %v", err)
		return 0
	}

	// Parse CIP response header
	if len(respData) < 4 {
		logging.DebugLog("EIP/Discovery", "Symbol table response too short: %d bytes", len(respData))
		return 0
	}

	status := respData[2]
	if status != statusSuccess {
		logging.DebugLog("EIP/Discovery", "Symbol table query status error: 0x%02X (%s)",
			status, cipStatusMessage(status))
		return 0
	}

	// Skip CIP header to get attribute data
	addlStatusSize := int(respData[3]) * 2
	dataStart := 4 + addlStatusSize
	if dataStart >= len(respData) {
		logging.DebugLog("EIP/Discovery", "Symbol table response has no data after header")
		return 0
	}

	attrData := respData[dataStart:]
	logging.DebugLog("EIP/Discovery", "Symbol table attributes (%d bytes): %X", len(attrData), attrData)

	// The Symbol Table class typically returns 4 UINT values (8 bytes):
	// - Attribute 1: Number of instances
	// - Attribute 2: Max instance (highest instance ID)
	// - Attribute 3: (varies)
	// - Attribute 4: (varies)
	// All values are often the same for a simple symbol table.
	if len(attrData) >= 2 {
		instanceCount := binary.LittleEndian.Uint16(attrData[0:2])
		logging.DebugLog("EIP/Discovery", "Symbol table reports %d instances", instanceCount)
		return uint32(instanceCount)
	}

	return 0
}

// getSymbolInstance reads a single symbol instance using Get Attributes All (0x01).
// This is the Omron-compatible method for tag discovery.
func (c *Client) getSymbolInstance(instance uint32) (TagInfo, error) {
	tag := TagInfo{Instance: instance}

	// Build path to Symbol Object (class 0x6B) with specific instance
	var path cip.EPath_t
	if instance <= 0xFF {
		path, _ = cip.EPath().Class(classSymbol).Instance(byte(instance)).Build()
	} else if instance <= 0xFFFF {
		path, _ = cip.EPath().Class(classSymbol).Instance16(uint16(instance)).Build()
	} else {
		path, _ = cip.EPath().Class(classSymbol).Instance32(instance).Build()
	}

	req := cip.Request{
		Service: svcGetAttributesAll,
		Path:    path,
	}

	respData, err := c.sendCIPRequest(req)
	if err != nil {
		return tag, fmt.Errorf("instance %d: %w", instance, err)
	}

	// Parse CIP response header
	if len(respData) < 4 {
		return tag, fmt.Errorf("instance %d: response too short (%d bytes)", instance, len(respData))
	}

	replyService := respData[0]
	status := respData[2]
	addlStatusSize := int(respData[3]) * 2

	// Verify it's a reply to Get Attributes All
	if replyService != (svcGetAttributesAll | 0x80) {
		return tag, fmt.Errorf("instance %d: unexpected service 0x%02X", instance, replyService)
	}

	// Check status
	if status != statusSuccess {
		return tag, fmt.Errorf("instance %d: CIP status 0x%02X (%s)",
			instance, status, cipStatusMessage(status))
	}

	// Skip header to get attribute data
	dataStart := 4 + addlStatusSize
	if dataStart >= len(respData) {
		return tag, fmt.Errorf("instance %d: no attribute data", instance)
	}

	attrData := respData[dataStart:]

	// Parse Omron-specific symbol attributes
	tag = c.parseOmronSymbolAttributes(attrData, instance)

	return tag, nil
}

// parseOmronSymbolAttributes parses the attribute data from Get Attributes All
// response for an Omron Symbol Object instance.
//
// Omron Symbol Object attributes (based on libplctag research and Wireshark captures):
//   - Name: STRING (1-byte length prefix + chars + optional padding byte if odd)
//   - Type: UINT (2 bytes, little-endian) - CIP data type code
//   - Additional attributes may follow (dimensions, byte count, etc.)
//
// NOTE: This format differs from Allen-Bradley Logix which uses a different
// attribute layout. The exact format may vary between Omron firmware versions.
func (c *Client) parseOmronSymbolAttributes(data []byte, instance uint32) TagInfo {
	tag := TagInfo{Instance: instance}

	if len(data) == 0 {
		return tag
	}

	logging.DebugLog("EIP/Discovery", "Parsing symbol instance %d: %d bytes: %X",
		instance, len(data), data)

	i := 0

	// Parse name: 1-byte length prefix followed by chars
	if i >= len(data) {
		return tag
	}
	nameLen := int(data[i])
	i++

	// Sanity check name length
	if nameLen <= 0 || nameLen > 255 || i+nameLen > len(data) {
		logging.DebugLog("EIP/Discovery", "Instance %d: invalid name length %d", instance, nameLen)
		return tag
	}

	tag.Name = string(data[i : i+nameLen])
	i += nameLen

	// Omron uses odd-byte padding: if name length is odd, skip 1 padding byte
	if nameLen%2 == 1 && i < len(data) {
		i++ // Skip padding byte
	}

	// Parse type code (2 bytes, little-endian)
	if i+2 > len(data) {
		logging.DebugLog("EIP/Discovery", "Instance %d (%s): no type code", instance, tag.Name)
		return tag
	}
	tag.TypeCode = binary.LittleEndian.Uint16(data[i : i+2])
	i += 2

	// Parse dimensions if this is an array type
	if IsArray(tag.TypeCode) && i+4 <= len(data) {
		// Try to read dimension count or element count
		// Format varies - try reading as element count first
		dimOrCount := binary.LittleEndian.Uint32(data[i : i+4])
		if dimOrCount > 0 && dimOrCount < 1000000 { // Reasonable array size
			tag.Dimensions = []uint32{dimOrCount}
			logging.DebugLog("EIP/Discovery", "Instance %d (%s): array with %d elements",
				instance, tag.Name, dimOrCount)
		}
	}

	logging.DebugLog("EIP/Discovery", "Instance %d: name=%q type=0x%04X (%s) dims=%v",
		instance, tag.Name, tag.TypeCode, TypeName(tag.TypeCode), tag.Dimensions)

	return tag
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// allTagsEIP discovers all tags using the Omron-specific method.
// Uses Get Attributes All (0x01) on individual Symbol Object instances.
func (c *Client) allTagsEIP() ([]TagInfo, error) {
	logging.DebugLog("EIP/Discovery", "allTagsEIP: using Omron-specific instance iteration method")
	return c.listSymbols()
}

// allTagsEIPFallback is a fallback method if the primary discovery fails.
// It uses a simpler approach with smaller instance IDs.
func (c *Client) allTagsEIPFallback() ([]TagInfo, error) {
	logging.DebugLog("EIP/Discovery", "allTagsEIPFallback: using simplified instance iteration")

	var tags []TagInfo
	consecutiveErrors := 0
	const maxConsecutiveErrors = 10

	// Use smaller instance limit for fallback
	for instance := uint32(1); instance <= 1000; instance++ {
		tag, err := c.getSymbolInstance(instance)
		if err != nil {
			consecutiveErrors++
			if instance <= 10 {
				logging.DebugLog("EIP/Discovery", "Fallback instance %d error: %v", instance, err)
			}
			if consecutiveErrors >= maxConsecutiveErrors {
				logging.DebugLog("EIP/Discovery", "Fallback: stopping after %d consecutive errors", consecutiveErrors)
				break
			}
			continue
		}

		consecutiveErrors = 0

		// Skip empty/system tags
		if tag.Name == "" {
			continue
		}
		if len(tag.Name) > 0 && (tag.Name[0] == '_' || tag.Name[0] == '$') {
			continue
		}

		tags = append(tags, tag)
		if len(tags) <= 10 {
			logging.DebugLog("EIP/Discovery", "Fallback: found tag %q (type=0x%04X)", tag.Name, tag.TypeCode)
		}
	}

	logging.DebugLog("EIP/Discovery", "Fallback complete: found %d tags", len(tags))
	return tags, nil
}
