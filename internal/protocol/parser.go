package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// ErrInvalidPacket is returned when a raw buffer does not look like a valid
// VT200 packet (bad start bytes, wrong CRC, truncated, etc.).
type ErrInvalidPacket struct {
	Reason string
}

func (e *ErrInvalidPacket) Error() string {
	return "invalid packet: " + e.Reason
}

// ReadPacket reads exactly one VT200 packet from r and returns the parsed
// Packet.  It blocks until a complete packet is available or an error occurs.
func ReadPacket(r io.Reader) (*Packet, error) {
	// Read the two start bytes.
	header := make([]byte, 2)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	var length int
	switch {
	case header[0] == StartByte1 && header[1] == StartByte1:
		// Short frame: 1-byte length.
		lb := make([]byte, 1)
		if _, err := io.ReadFull(r, lb); err != nil {
			return nil, err
		}
		length = int(lb[0])
	case header[0] == StartByte1L && header[1] == StartByte1L:
		// Long frame: 2-byte big-endian length.
		lb := make([]byte, 2)
		if _, err := io.ReadFull(r, lb); err != nil {
			return nil, err
		}
		length = int(binary.BigEndian.Uint16(lb))
	default:
		return nil, &ErrInvalidPacket{Reason: fmt.Sprintf("unexpected start bytes 0x%02X 0x%02X", header[0], header[1])}
	}

	if length < 4 {
		return nil, &ErrInvalidPacket{Reason: "length field too small"}
	}

	// Read the rest: [protocol][content…][serial(2)][crc(2)][0x0D 0x0A]
	// The length field covers protocol + content + serial + crc; the
	// terminator (0x0D 0x0A) follows and adds 2 more bytes.
	rest := make([]byte, length+2)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}

	// Validate terminator.
	if rest[len(rest)-2] != EndByte1 || rest[len(rest)-1] != EndByte2 {
		return nil, &ErrInvalidPacket{Reason: "missing end bytes"}
	}

	// Layout inside rest (before terminator):
	//   [protocol(1)][content(length-4)][serial(2)][crc(2)]
	body := rest[:len(rest)-2] // strip terminator

	if len(body) < 4 {
		return nil, &ErrInvalidPacket{Reason: "body too short"}
	}

	protocol := body[0]
	content := body[1 : len(body)-4]
	serialBytes := body[len(body)-4 : len(body)-2]
	crcBytes := body[len(body)-2:]

	serialNo := binary.BigEndian.Uint16(serialBytes)

	// Validate CRC over [protocol][content][serial].
	crcData := body[:len(body)-2]
	computedCRC := crc16(crcData)
	receivedCRC := binary.BigEndian.Uint16(crcBytes)
	if computedCRC != receivedCRC {
		return nil, &ErrInvalidPacket{
			Reason: fmt.Sprintf("CRC mismatch: got 0x%04X want 0x%04X", receivedCRC, computedCRC),
		}
	}

	return parseContent(protocol, serialNo, content)
}

// ParsePacket parses a complete raw frame (including start bytes and
// terminator) from a byte slice.  Useful for unit testing without an
// io.Reader.
func ParsePacket(raw []byte) (*Packet, error) {
	if len(raw) < 9 {
		return nil, &ErrInvalidPacket{Reason: "packet too short"}
	}

	var length int
	var offset int

	switch {
	case raw[0] == StartByte1 && raw[1] == StartByte1:
		length = int(raw[2])
		offset = 3
	case raw[0] == StartByte1L && raw[1] == StartByte1L:
		length = int(binary.BigEndian.Uint16(raw[2:4]))
		offset = 4
	default:
		return nil, &ErrInvalidPacket{Reason: fmt.Sprintf("unexpected start bytes 0x%02X 0x%02X", raw[0], raw[1])}
	}

	// Expected total size: offset + length + 2 (terminator)
	expected := offset + length + 2
	if len(raw) < expected {
		return nil, &ErrInvalidPacket{Reason: "truncated packet"}
	}

	// Validate terminator.
	if raw[expected-2] != EndByte1 || raw[expected-1] != EndByte2 {
		return nil, &ErrInvalidPacket{Reason: "missing end bytes"}
	}

	body := raw[offset : offset+length]
	if len(body) < 4 {
		return nil, &ErrInvalidPacket{Reason: "body too short"}
	}

	protocol := body[0]
	content := body[1 : len(body)-4]
	serialBytes := body[len(body)-4 : len(body)-2]
	crcBytes := body[len(body)-2:]

	serialNo := binary.BigEndian.Uint16(serialBytes)

	crcData := body[:len(body)-2]
	computedCRC := crc16(crcData)
	receivedCRC := binary.BigEndian.Uint16(crcBytes)
	if computedCRC != receivedCRC {
		return nil, &ErrInvalidPacket{
			Reason: fmt.Sprintf("CRC mismatch: got 0x%04X want 0x%04X", receivedCRC, computedCRC),
		}
	}

	return parseContent(protocol, serialNo, content)
}

// BuildResponse constructs the server acknowledgement packet for a given
// protocol number and serial number.
//
// Response format:
//
//	[0x78][0x78][0x05][protocol][serial_hi][serial_lo][crc_hi][crc_lo][0x0D][0x0A]
func BuildResponse(protocol byte, serialNo uint16) []byte {
	buf := make([]byte, 10)
	buf[0] = StartByte1
	buf[1] = StartByte1
	buf[2] = 0x05 // length: protocol(1) + serial(2) + crc(2)
	buf[3] = protocol
	binary.BigEndian.PutUint16(buf[4:6], serialNo)

	crc := crc16(buf[3:6])
	binary.BigEndian.PutUint16(buf[6:8], crc)
	buf[8] = EndByte1
	buf[9] = EndByte2
	return buf
}

// parseContent dispatches to the appropriate field parser based on the protocol
// number and returns a populated Packet.
func parseContent(protocol byte, serialNo uint16, content []byte) (*Packet, error) {
	pkt := &Packet{
		Protocol:   protocol,
		SerialNo:   serialNo,
		RawPayload: content,
	}

	switch protocol {
	case ProtocolLogin:
		login, err := parseLogin(content)
		if err != nil {
			return nil, err
		}
		pkt.Type = PacketTypeLogin
		pkt.Login = login

	case ProtocolHeartbeat, ProtocolStatus:
		pkt.Type = PacketTypeHeartbeat

	case ProtocolGPS, ProtocolGPSLBS, ProtocolGPSLBSExt:
		gps, err := parseGPS(content)
		if err != nil {
			return nil, err
		}
		pkt.Type = PacketTypeGPS
		pkt.GPS = gps

	case ProtocolAlarm:
		gps, err := parseAlarm(content)
		if err != nil {
			return nil, err
		}
		pkt.Type = PacketTypeGPSAlarm
		pkt.GPS = gps

	default:
		pkt.Type = PacketTypeUnknown
	}

	return pkt, nil
}

// parseLogin decodes the IMEI from a login packet content.
// The IMEI is encoded as 8 BCD bytes (first nibble of first byte is ignored).
func parseLogin(content []byte) (*LoginData, error) {
	if len(content) < 8 {
		return nil, &ErrInvalidPacket{Reason: "login content too short"}
	}
	imei := bcdToIMEI(content[:8])
	return &LoginData{IMEI: imei}, nil
}

// parseGPS decodes a standard GPS location content block.
//
// Content layout (minimum 12 bytes):
//
//	[YY][MM][DD][HH][MM][SS][info_len][lat(4)][lon(4)][speed(1)][course_flags(2)]
//	[mcc(2)][mnc(1)][lac(2)][cell_id(3)][signal(1)][voltage(1)]
func parseGPS(content []byte) (*GPSData, error) {
	if len(content) < 12 {
		return nil, &ErrInvalidPacket{Reason: "GPS content too short"}
	}

	ts, err := parseTimestamp(content[0:6])
	if err != nil {
		return nil, err
	}

	infoLen := int(content[6]) // bit-field describing what follows
	satellites := uint8(infoLen & 0x0F)
	gpsValid := (infoLen>>4)&0x01 == 1

	if len(content) < 18 {
		return nil, &ErrInvalidPacket{Reason: "GPS content truncated before coordinates"}
	}

	latRaw := binary.BigEndian.Uint32(content[7:11])
	lonRaw := binary.BigEndian.Uint32(content[11:15])
	lat := float64(latRaw) / 1800000.0
	lon := float64(lonRaw) / 1800000.0

	speed := content[15]
	courseFlags := binary.BigEndian.Uint16(content[16:18])
	course := courseFlags & 0x03FF // lower 10 bits

	// Direction flags (bits 10–13 of courseFlags)
	if (courseFlags>>10)&0x01 == 0 {
		lat = -lat // South
	}
	if (courseFlags>>11)&0x01 != 0 {
		lon = -lon // West
	}
	realtime := (courseFlags>>13)&0x01 == 1

	gps := &GPSData{
		Timestamp:  ts,
		Latitude:   lat,
		Longitude:  lon,
		Speed:      speed,
		Course:     course,
		Satellites: satellites,
		GPSValid:   gpsValid,
		Realtime:   realtime,
	}

	// Optional LBS tail.
	if len(content) >= 26 {
		gps.MCC = binary.BigEndian.Uint16(content[18:20])
		gps.MNC = content[20]
		gps.LAC = binary.BigEndian.Uint16(content[21:23])
		gps.CellID = uint32(content[23])<<16 | uint32(content[24])<<8 | uint32(content[25])
	}
	if len(content) >= 28 {
		gps.Signal = content[26]
		gps.Voltage = content[27]
	}

	return gps, nil
}

// parseAlarm decodes an alarm packet; the layout is the same as GPS with an
// additional alarm-type byte at the start of the content.
func parseAlarm(content []byte) (*GPSData, error) {
	if len(content) < 1 {
		return nil, &ErrInvalidPacket{Reason: "alarm content too short"}
	}
	alarmType := content[0]
	gps, err := parseGPS(content[1:])
	if err != nil {
		return nil, err
	}
	gps.AlarmType = alarmType
	return gps, nil
}

// parseTimestamp converts 6 bytes [YY MM DD HH MM SS] to a time.Time (UTC).
func parseTimestamp(b []byte) (time.Time, error) {
	if len(b) < 6 {
		return time.Time{}, &ErrInvalidPacket{Reason: "timestamp too short"}
	}
	year := 2000 + int(b[0])
	month := time.Month(b[1])
	day := int(b[2])
	hour := int(b[3])
	min := int(b[4])
	sec := int(b[5])
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC), nil
}

// bcdToIMEI converts 8 BCD-encoded bytes into a 15-character IMEI string.
func bcdToIMEI(b []byte) string {
	digits := make([]byte, 0, 16)
	for _, v := range b {
		hi := (v >> 4) & 0x0F
		lo := v & 0x0F
		digits = append(digits, '0'+hi, '0'+lo)
	}
	// The 16th nibble (last nibble) is padding; return only 15 digits.
	if len(digits) > 15 {
		digits = digits[:15]
	}
	return string(digits)
}

// crc16 computes the CRC-16/IBM (CRC-16-ANSI, polynomial 0x8005) checksum.
func crc16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x8005
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
