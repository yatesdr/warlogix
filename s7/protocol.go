package s7

import (
	"encoding/binary"
	"fmt"
)

const (
	s7ProtocolID = 0x32

	// Message Types
	s7MsgJob     = 0x01
	s7MsgAckData = 0x03

	// Functions
	s7FuncSetupComm = 0xF0
	s7FuncRead      = 0x04
	s7FuncWrite     = 0x05

	// Area Codes (for S7ANY addressing)
	s7AreaSysInfo = 0x03 // System info
	s7AreaSysFlg  = 0x05 // System flags
	s7AreaAnaIn   = 0x06 // Analog inputs
	s7AreaAnaOut  = 0x07 // Analog outputs
	s7AreaC       = 0x1C // Counters (S7-200/300)
	s7AreaT       = 0x1D // Timers (S7-200/300)
	s7AreaC200    = 0x1E // IEC counters (S7-200)
	s7AreaT200    = 0x1F // IEC timers (S7-200)
	s7AreaI       = 0x81 // Inputs
	s7AreaQ       = 0x82 // Outputs
	s7AreaM       = 0x83 // Markers/Flags
	s7AreaDB      = 0x84 // Data blocks
	s7AreaDI      = 0x85 // Instance data blocks
	s7AreaLocal   = 0x86 // Local data
	s7AreaV       = 0x87 // V memory (S7-200)

	// Transport sizes for S7ANY
	tsNULL  = 0x00
	tsBIT   = 0x01
	tsBYTE  = 0x02
	tsCHAR  = 0x03
	tsWORD  = 0x04
	tsINT   = 0x05
	tsDWORD = 0x06
	tsDINT  = 0x07
	tsREAL  = 0x08

	// S7ANY constants
	s7AnySpecType = 0x12
	s7AnyLen      = 0x0A
	s7AnySyntaxID = 0x10
)

// buildSetupCommRequest creates an S7 Setup Communication request PDU.
func buildSetupCommRequest(pduSize uint16) []byte {
	// S7 Header (10 bytes for Job)
	header := []byte{
		s7ProtocolID, // Protocol ID
		s7MsgJob,     // Message type: Job
		0x00, 0x00,   // Reserved
		0x00, 0x00,   // PDU reference
		0x00, 0x08,   // Parameter length: 8 bytes
		0x00, 0x00,   // Data length: 0
	}

	// Setup Communication parameters (8 bytes)
	params := []byte{
		s7FuncSetupComm, // Function: Setup Communication
		0x00,            // Reserved
		0x00, 0x01,      // Max AMQ calling
		0x00, 0x01,      // Max AMQ called
		byte(pduSize >> 8), byte(pduSize), // PDU size
	}

	return append(header, params...)
}

// parseSetupCommResponse parses an S7 Setup Communication response.
func parseSetupCommResponse(data []byte) (uint16, error) {
	// Minimum: 10 byte header + 8 byte params = 18 bytes
	// Response header is 12 bytes (includes error class/code)
	if len(data) < 20 {
		return 0, fmt.Errorf("setup response too short: %d bytes", len(data))
	}

	// Check protocol ID
	if data[0] != s7ProtocolID {
		return 0, fmt.Errorf("invalid protocol ID: 0x%02X", data[0])
	}

	// Check message type
	if data[1] != s7MsgAckData {
		return 0, fmt.Errorf("unexpected message type: 0x%02X", data[1])
	}

	// Check error class/code (bytes 10-11 in response)
	if data[10] != 0 || data[11] != 0 {
		return 0, S7Error{Class: data[10], Code: data[11]}
	}

	// Check function code
	if data[12] != s7FuncSetupComm {
		return 0, fmt.Errorf("unexpected function: 0x%02X", data[12])
	}

	// Extract negotiated PDU size (last 2 bytes of params)
	pduSize := binary.BigEndian.Uint16(data[18:20])
	return pduSize, nil
}

// buildReadRequest creates an S7 Read Variable request PDU.
func buildReadRequest(addrs []*Address, pduRef uint16) []byte {
	itemCount := len(addrs)

	// S7 Header (10 bytes for Job)
	paramLen := 2 + itemCount*12 // function + count + items
	header := []byte{
		s7ProtocolID,              // Protocol ID
		s7MsgJob,                  // Message type: Job
		0x00, 0x00,                // Reserved
		byte(pduRef >> 8), byte(pduRef), // PDU reference
		byte(paramLen >> 8), byte(paramLen), // Parameter length
		0x00, 0x00, // Data length: 0 for read request
	}

	// Parameters: function + count + items
	params := []byte{
		s7FuncRead,      // Function: Read Variable
		byte(itemCount), // Item count
	}

	// Add S7ANY items
	for _, addr := range addrs {
		item := addressToS7Any(addr)
		params = append(params, item...)
	}

	return append(header, params...)
}

// parseReadResponse parses an S7 Read Variable response.
func parseReadResponse(data []byte, count int) ([][]byte, []error) {
	results := make([][]byte, count)
	errors := make([]error, count)

	// Minimum header size check
	if len(data) < 12 {
		for i := range errors {
			errors[i] = fmt.Errorf("response too short")
		}
		return results, errors
	}

	// Check protocol ID
	if data[0] != s7ProtocolID {
		for i := range errors {
			errors[i] = fmt.Errorf("invalid protocol ID: 0x%02X", data[0])
		}
		return results, errors
	}

	// Check message type
	if data[1] != s7MsgAckData {
		for i := range errors {
			errors[i] = fmt.Errorf("unexpected message type: 0x%02X", data[1])
		}
		return results, errors
	}

	// Check error class/code
	if data[10] != 0 || data[11] != 0 {
		err := S7Error{Class: data[10], Code: data[11]}
		for i := range errors {
			errors[i] = err
		}
		return results, errors
	}

	// Get parameter and data lengths
	paramLen := binary.BigEndian.Uint16(data[6:8])
	dataLen := binary.BigEndian.Uint16(data[8:10])

	// Skip header (12 bytes) and parameters
	dataStart := 12 + int(paramLen)
	if dataStart > len(data) || int(dataLen) > len(data)-dataStart {
		for i := range errors {
			errors[i] = fmt.Errorf("invalid response lengths")
		}
		return results, errors
	}

	// Parse data items
	pos := dataStart
	for i := 0; i < count; i++ {
		if pos >= len(data) {
			errors[i] = fmt.Errorf("unexpected end of data")
			continue
		}

		// Data item header: return code, transport size, length
		returnCode := data[pos]
		if returnCode != dataItemSuccess {
			errors[i] = fmt.Errorf("%s", dataItemError(returnCode))
			// Skip past this item - look for next item
			// For error items, typically 1 byte (just the return code)
			pos++
			continue
		}

		// Success - parse data
		if pos+4 > len(data) {
			errors[i] = fmt.Errorf("data item header too short")
			continue
		}

		transportSize := data[pos+1]
		bitLen := binary.BigEndian.Uint16(data[pos+2 : pos+4])

		// Calculate byte length
		var byteLen int
		if transportSize == tsBIT || transportSize == tsINT || transportSize == tsDINT {
			// For bit and some types, length is in bits
			byteLen = int((bitLen + 7) / 8)
		} else {
			// For byte-oriented types, length is in bytes
			byteLen = int(bitLen)
		}

		pos += 4 // Skip header

		if pos+byteLen > len(data) {
			errors[i] = fmt.Errorf("data truncated")
			continue
		}

		results[i] = make([]byte, byteLen)
		copy(results[i], data[pos:pos+byteLen])
		pos += byteLen

		// Items are padded to even bytes (except last)
		if i < count-1 && byteLen%2 == 1 {
			pos++
		}
	}

	return results, errors
}

// buildWriteRequest creates an S7 Write Variable request PDU.
func buildWriteRequest(addr *Address, writeData []byte, pduRef uint16) []byte {
	// S7 Header (10 bytes for Job)
	paramLen := 2 + 12 // function + count + 1 item
	// Data: return code (1) + transport size (1) + length (2) + data
	dataLen := 4 + len(writeData)
	// Pad to even length
	if len(writeData)%2 == 1 {
		dataLen++
	}

	header := []byte{
		s7ProtocolID,              // Protocol ID
		s7MsgJob,                  // Message type: Job
		0x00, 0x00,                // Reserved
		byte(pduRef >> 8), byte(pduRef), // PDU reference
		byte(paramLen >> 8), byte(paramLen), // Parameter length
		byte(dataLen >> 8), byte(dataLen), // Data length
	}

	// Parameters: function + count + item
	params := []byte{
		s7FuncWrite, // Function: Write Variable
		0x01,        // Item count: 1
	}
	params = append(params, addressToS7Any(addr)...)

	// Data
	transportSize := getTransportSize(addr.DataType, addr.BitNum >= 0)
	bitLen := len(writeData) * 8
	if addr.BitNum >= 0 {
		bitLen = 1 // Single bit
	}

	dataSection := []byte{
		0x00,                                // Return code placeholder
		transportSize,                       // Transport size
		byte(bitLen >> 8), byte(bitLen),     // Bit length
	}
	dataSection = append(dataSection, writeData...)

	// Pad to even length
	if len(writeData)%2 == 1 {
		dataSection = append(dataSection, 0x00)
	}

	result := append(header, params...)
	result = append(result, dataSection...)
	return result
}

// parseWriteResponse parses an S7 Write Variable response.
func parseWriteResponse(data []byte) error {
	// Minimum header size check
	if len(data) < 12 {
		return fmt.Errorf("response too short")
	}

	// Check protocol ID
	if data[0] != s7ProtocolID {
		return fmt.Errorf("invalid protocol ID: 0x%02X", data[0])
	}

	// Check message type
	if data[1] != s7MsgAckData {
		return fmt.Errorf("unexpected message type: 0x%02X", data[1])
	}

	// Check error class/code
	if data[10] != 0 || data[11] != 0 {
		return S7Error{Class: data[10], Code: data[11]}
	}

	// Get parameter length to find data section
	paramLen := binary.BigEndian.Uint16(data[6:8])
	dataStart := 12 + int(paramLen)

	if dataStart >= len(data) {
		return fmt.Errorf("no data in response")
	}

	// Check data item return code
	returnCode := data[dataStart]
	if returnCode != dataItemSuccess {
		return fmt.Errorf("%s", dataItemError(returnCode))
	}

	return nil
}

// addressToS7Any converts an Address to S7ANY item bytes.
func addressToS7Any(addr *Address) []byte {
	// Determine area code
	var areaCode byte
	switch addr.Area {
	case AreaI:
		areaCode = s7AreaI
	case AreaQ:
		areaCode = s7AreaQ
	case AreaM:
		areaCode = s7AreaM
	case AreaDB:
		areaCode = s7AreaDB
	case AreaT:
		areaCode = s7AreaT
	case AreaC:
		areaCode = s7AreaC
	default:
		areaCode = s7AreaDB
	}

	// Determine transport size and count
	transportSize := getTransportSize(addr.DataType, addr.BitNum >= 0)
	count := addr.Size
	if addr.BitNum >= 0 {
		count = 1 // Single bit
	}

	// For byte-based reads, S7 expects element count, not byte count
	// We specify transport size and let count be the number of bytes
	if transportSize == tsBYTE {
		// count is already in bytes, which is what we want
	}

	// Encode address: (byte_offset << 3) | bit_number
	// 24-bit big-endian
	bitAddr := addr.Offset * 8
	if addr.BitNum >= 0 {
		bitAddr += addr.BitNum
	}

	dbNumber := addr.DBNumber
	if addr.Area != AreaDB {
		dbNumber = 0
	}

	return []byte{
		s7AnySpecType, // Specification type
		s7AnyLen,      // Length of this item
		s7AnySyntaxID, // Syntax ID: S7ANY
		transportSize, // Transport size
		byte(count >> 8), byte(count), // Count
		byte(dbNumber >> 8), byte(dbNumber), // DB number
		areaCode,                                                     // Area
		byte(bitAddr >> 16), byte(bitAddr >> 8), byte(bitAddr), // Address (24-bit)
	}
}

// getTransportSize returns the S7 transport size code for a data type.
func getTransportSize(dataType uint16, isBit bool) byte {
	if isBit {
		return tsBIT
	}

	baseType := BaseType(dataType)
	switch baseType {
	case TypeBool:
		return tsBIT
	case TypeByte, TypeSInt, TypeChar:
		return tsBYTE
	case TypeWord, TypeInt, TypeDate, TypeWChar:
		return tsWORD
	case TypeDWord, TypeDInt, TypeTime, TypeTimeOfDay:
		return tsDWORD
	case TypeReal:
		return tsREAL
	case TypeLWord, TypeLInt, TypeLReal:
		// 64-bit types - use BYTE transport
		return tsBYTE
	default:
		return tsBYTE
	}
}
