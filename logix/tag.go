package logix

import (
	"encoding/binary"
	"fmt"

	"warlogix/cip"
)

// TagInfo contains metadata about a tag from the PLC's symbol table.
type TagInfo struct {
	Name       string // Full tag name (e.g., "MyTag" or "Program:MainProgram.MyTag")
	TypeCode   uint16 // CIP data type code
	Instance   uint32 // Symbol instance ID (used for pagination)
	Dimensions []int  // Array dimensions (nil for scalar)
}

// IsProgram returns true if this tag represents a program entry (not a program-scoped tag).
// Program entries look like "Program:MainProgram" (no dot after program name).
// Program-scoped tags look like "Program:MainProgram.TagName" (have a dot).
func (t TagInfo) IsProgram() bool {
	if len(t.Name) < 8 || t.Name[:8] != "Program:" {
		return false
	}
	// Check if there's a dot after "Program:" - if so, it's a tag, not a program entry
	for i := 8; i < len(t.Name); i++ {
		if t.Name[i] == '.' {
			return false // This is a program-scoped tag, not a program entry
		}
	}
	return true // No dot found - this is a program entry like "Program:MainProgram"
}

// IsSystem returns true if this is a system/internal tag (Map:, Task:, Cxn:, etc.)
func (t TagInfo) IsSystem() bool {
	if len(t.Name) >= 4 {
		prefix := t.Name[:4]
		if prefix == "Map:" || prefix == "Cxn:" {
			return true
		}
	}
	if len(t.Name) >= 5 && t.Name[:5] == "Task:" {
		return true
	}
	return false
}

// IsRoutine returns true if this is a routine entry (not a readable tag).
func (t TagInfo) IsRoutine() bool {
	// Check for ".Routine:" pattern anywhere in the name
	for i := 0; i < len(t.Name)-8; i++ {
		if t.Name[i:i+8] == "Routine:" {
			return true
		}
	}
	return false
}

// IsReadable returns true if this tag can be read/written (not a program, routine, or system entry).
func (t TagInfo) IsReadable() bool {
	return !t.IsProgram() && !t.IsRoutine() && !t.IsSystem()
}

// TypeName returns the human-readable type name.
func (t TagInfo) TypeName() string {
	return TypeName(t.TypeCode)
}

// ListTags returns all controller-scope tags and program entries.
// Use ListProgramTags to get tags within a specific program.
func (p *PLC) ListTags() ([]TagInfo, error) {
	return p.listSymbols("", 0)
}

// ListPrograms returns just the program names from the PLC.
// Returns names like "Program:MainProgram", "Program:SafetyProgram", etc.
func (p *PLC) ListPrograms() ([]string, error) {
	tags, err := p.ListTags()
	if err != nil {
		return nil, err
	}

	var programs []string
	seen := make(map[string]bool)
	for _, t := range tags {
		if t.IsProgram() && !seen[t.Name] {
			seen[t.Name] = true
			programs = append(programs, t.Name)
		}
	}
	return programs, nil
}

// ListProgramTags returns all tags within a specific program.
// programName should be just the program name (e.g., "MainProgram"),
// or the full form (e.g., "Program:MainProgram").
func (p *PLC) ListProgramTags(programName string) ([]TagInfo, error) {
	// Normalize the program name
	if len(programName) < 8 || programName[:8] != "Program:" {
		programName = "Program:" + programName
	}
	return p.listSymbols(programName, 0)
}

// ListDataTags returns only readable/writable data tags, excluding programs, routines, and system tags.
func (p *PLC) ListDataTags() ([]TagInfo, error) {
	allTags, err := p.ListAllTags()
	if err != nil {
		return nil, err
	}

	var dataTags []TagInfo
	for _, t := range allTags {
		if t.IsReadable() {
			dataTags = append(dataTags, t)
		}
	}
	return dataTags, nil
}

// ListAllTags returns all tags: controller-scope, program entries, and tags within each program.
func (p *PLC) ListAllTags() ([]TagInfo, error) {
	// First, get controller-scope tags and program entries
	baseTags, err := p.ListTags()
	if err != nil {
		return nil, fmt.Errorf("ListAllTags: %w", err)
	}

	// Find all program entries
	var programs []string
	seen := make(map[string]bool)
	for _, t := range baseTags {
		if t.IsProgram() && !seen[t.Name] {
			seen[t.Name] = true
			programs = append(programs, t.Name)
		}
	}

	// Collect all tags
	allTags := make([]TagInfo, 0, len(baseTags))
	allTags = append(allTags, baseTags...)

	// Browse each program's tags
	for _, prog := range programs {
		progTags, err := p.listSymbols(prog, 0)
		if err != nil {
			// Skip programs that can't be browsed
			continue
		}

		// Prefix tags with program name if not already prefixed
		prefix := prog + "."
		for i := range progTags {
			if len(progTags[i].Name) < len(prefix) || progTags[i].Name[:len(prefix)] != prefix {
				// Check if already has any Program: prefix
				if len(progTags[i].Name) < 8 || progTags[i].Name[:8] != "Program:" {
					progTags[i].Name = prefix + progTags[i].Name
				}
			}
		}

		allTags = append(allTags, progTags...)
	}

	return allTags, nil
}

// listSymbols queries the Symbol Object (class 0x6B) for tag information.
// scope: "" for controller scope, or "Program:ProgramName" for program scope
// startInstance: starting instance for pagination (0 for first page)
func (p *PLC) listSymbols(scope string, startInstance uint32) ([]TagInfo, error) {
	if p == nil || p.Connection == nil {
		return nil, fmt.Errorf("listSymbols: nil plc or connection")
	}

	var allTags []TagInfo
	instance := startInstance

	// Pagination loop - limit to prevent infinite loops
	for page := 0; page < 1000; page++ {
		tags, lastInstance, hasMore, err := p.listSymbolsPage(scope, instance)
		if err != nil {
			return nil, err
		}

		allTags = append(allTags, tags...)

		if !hasMore || len(tags) == 0 {
			break
		}

		// Next page starts after the last instance we received
		instance = lastInstance + 1
	}

	return allTags, nil
}

// listSymbolsPage fetches one page of symbols.
func (p *PLC) listSymbolsPage(scope string, startInstance uint32) (tags []TagInfo, lastInstance uint32, hasMore bool, err error) {
	// Build the request path
	path, err := p.buildSymbolPath(scope, startInstance)
	if err != nil {
		return nil, 0, false, fmt.Errorf("buildSymbolPath: %w", err)
	}

	// Request attributes: name (1), type (2)
	// Attribute 8 (byte count) is also useful but optional
	attrData := []byte{
		0x02, 0x00, // Attribute count: 2
		0x01, 0x00, // Attribute 1: Symbol Name
		0x02, 0x00, // Attribute 2: Symbol Type
	}

	// Build the CIP request
	reqData := make([]byte, 0, 2+len(path)+len(attrData))
	reqData = append(reqData, SvcGetInstanceAttributeList) // Service 0x55
	reqData = append(reqData, path.WordLen())
	reqData = append(reqData, path...)
	reqData = append(reqData, attrData...)

	// Send request using connected or unconnected messaging
	cipResp, err := p.sendCipRequest(reqData)
	if err != nil {
		return nil, 0, false, err
	}

	// Parse response header
	if len(cipResp) < 4 {
		return nil, 0, false, fmt.Errorf("response too short: %d bytes", len(cipResp))
	}

	replyService := cipResp[0]
	status := cipResp[2]
	addlStatusSize := cipResp[3]

	// Verify service
	if replyService != (SvcGetInstanceAttributeList | 0x80) {
		return nil, 0, false, fmt.Errorf("unexpected reply service: 0x%02X", replyService)
	}

	// Check for "partial transfer" (more data available)
	hasMore = (status == StatusPartialTransfer)

	// Check for errors (but partial transfer is OK)
	if status != StatusSuccess && status != StatusPartialTransfer {
		return nil, 0, false, parseCipError(status, addlStatusSize, cipResp[4:])
	}

	// Parse tag data (skip 4-byte header + additional status)
	dataStart := 4 + int(addlStatusSize)*2
	if dataStart > len(cipResp) {
		return nil, 0, hasMore, nil // No data
	}

	tags, lastInstance = parseSymbolListResponse(cipResp[dataStart:])
	return tags, lastInstance, hasMore, nil
}

// buildSymbolPath builds the EPath for symbol listing.
func (p *PLC) buildSymbolPath(scope string, startInstance uint32) (cip.EPath_t, error) {
	builder := cip.EPath()

	// Add program scope if specified
	if scope != "" {
		builder = builder.Symbol(scope)
	}

	// Add Symbol Object class (0x6B)
	builder = builder.Class(0x6B)

	// Add instance (using 16-bit if > 255)
	if startInstance <= 0xFF {
		builder = builder.Instance(byte(startInstance))
	} else if startInstance <= 0xFFFF {
		builder = builder.Instance16(uint16(startInstance))
	} else {
		return nil, fmt.Errorf("instance %d exceeds 16-bit maximum", startInstance)
	}

	return builder.Build()
}

// parseSymbolListResponse parses the tag list data from Get Instance Attribute List response.
// Response format per tag:
// - Instance ID (4 bytes, UDINT)
// - Attribute 1 data: Name length (2 bytes) + Name (n bytes)
// - Attribute 2 data: Symbol type (2 bytes)
func parseSymbolListResponse(data []byte) (tags []TagInfo, lastInstance uint32) {
	i := 0

	for i < len(data) {
		// Need at least: instance(4) + nameLen(2) + type(2) = 8 bytes minimum
		if i+8 > len(data) {
			break
		}

		// Instance ID (UDINT - 4 bytes)
		instance := binary.LittleEndian.Uint32(data[i : i+4])
		i += 4

		// Name length (UINT - 2 bytes)
		nameLen := int(binary.LittleEndian.Uint16(data[i : i+2]))
		i += 2

		// Check if we have enough data for name + type
		if i+nameLen+2 > len(data) {
			break
		}

		// Name (ASCII string)
		name := string(data[i : i+nameLen])
		i += nameLen

		// Symbol type (UINT - 2 bytes)
		typeCode := binary.LittleEndian.Uint16(data[i : i+2])
		i += 2

		// Skip if this looks like a partial/invalid entry
		if name == "" || instance == 0 {
			continue
		}

		tags = append(tags, TagInfo{
			Name:     name,
			TypeCode: typeCode,
			Instance: instance,
		})

		lastInstance = instance
	}

	return tags, lastInstance
}
