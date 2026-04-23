package protocol_test

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
)

// buildRawPacket constructs a well-formed VT200 short packet from the given
// protocol number and content bytes, computing the CRC and appending the
// terminator automatically.
func buildRawPacket(proto byte, content []byte, serialNo uint16) []byte {
	// body = [protocol(1)][content][serial(2)][crc(2)]
	bodyLen := 1 + len(content) + 2 + 2
	buf := make([]byte, 0, 3+bodyLen+2)

	// Start bytes + length
	buf = append(buf, protocol.StartByte1, protocol.StartByte1, byte(bodyLen))

	// Protocol
	buf = append(buf, proto)

	// Content
	buf = append(buf, content...)

	// Serial number
	sn := [2]byte{}
	binary.BigEndian.PutUint16(sn[:], serialNo)
	buf = append(buf, sn[:]...)

	// CRC over [protocol][content][serial]
	crcData := buf[3 : len(buf)] // from protocol byte onward
	crc := exportedCRC16(crcData)
	crcB := [2]byte{}
	binary.BigEndian.PutUint16(crcB[:], crc)
	buf = append(buf, crcB[:]...)

	// Terminator
	buf = append(buf, protocol.EndByte1, protocol.EndByte2)
	return buf
}

// exportedCRC16 re-implements the same CRC-16/IBM algorithm used by the
// package so tests do not depend on unexported symbols.
func exportedCRC16(data []byte) uint16 {
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

// --- Login packet tests ---

func buildLoginContent(imeiHex [8]byte) []byte {
	return imeiHex[:]
}

func TestParseLogin(t *testing.T) {
	// IMEI 123456789012345  → BCD: 12 34 56 78 90 12 34 5F
	imeiBytes := [8]byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12, 0x34, 0x5F}
	raw := buildRawPacket(protocol.ProtocolLogin, buildLoginContent(imeiBytes), 1)

	pkt, err := protocol.ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket error: %v", err)
	}

	if pkt.Type != protocol.PacketTypeLogin {
		t.Errorf("expected PacketTypeLogin, got %v", pkt.Type)
	}
	if pkt.Login == nil {
		t.Fatal("Login field is nil")
	}
	// First 15 BCD digits of 0x12 0x34 0x56 0x78 0x90 0x12 0x34 0x5F
	// → "123456789012345"
	want := "123456789012345"
	if pkt.Login.IMEI != want {
		t.Errorf("IMEI: got %q, want %q", pkt.Login.IMEI, want)
	}
	if pkt.SerialNo != 1 {
		t.Errorf("SerialNo: got %d, want 1", pkt.SerialNo)
	}
}

func TestParseLoginTooShort(t *testing.T) {
	raw := buildRawPacket(protocol.ProtocolLogin, []byte{0x12, 0x34}, 1)
	_, err := protocol.ParsePacket(raw)
	if err == nil {
		t.Fatal("expected error for truncated login content")
	}
}

// --- Heartbeat packet test ---

func TestParseHeartbeat(t *testing.T) {
	raw := buildRawPacket(protocol.ProtocolHeartbeat, []byte{}, 2)
	pkt, err := protocol.ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket error: %v", err)
	}
	if pkt.Type != protocol.PacketTypeHeartbeat {
		t.Errorf("expected PacketTypeHeartbeat, got %v", pkt.Type)
	}
}

// --- GPS packet tests ---

// buildGPSContent constructs a minimal GPS content block.
func buildGPSContent(ts time.Time, latDeg, lonDeg float64, speed uint8, course uint16, sats uint8, valid, north, east, realtime bool) []byte {
	buf := make([]byte, 18)

	// Timestamp
	buf[0] = byte(ts.Year() - 2000)
	buf[1] = byte(ts.Month())
	buf[2] = byte(ts.Day())
	buf[3] = byte(ts.Hour())
	buf[4] = byte(ts.Minute())
	buf[5] = byte(ts.Second())

	// Info byte: [valid(4 bits) | satellites(4 bits)]
	infoLen := sats & 0x0F
	if valid {
		infoLen |= 0x10
	}
	buf[6] = infoLen

	// Latitude / longitude (raw units = degrees * 1,800,000)
	latRaw := uint32(latDeg * 1800000)
	lonRaw := uint32(lonDeg * 1800000)
	binary.BigEndian.PutUint32(buf[7:11], latRaw)
	binary.BigEndian.PutUint32(buf[11:15], lonRaw)

	buf[15] = speed

	// Course + flags (bits 10–13)
	flags := course & 0x03FF
	if north {
		flags |= 1 << 10
	}
	if !east {
		flags |= 1 << 11
	}
	if realtime {
		flags |= 1 << 13
	}
	binary.BigEndian.PutUint16(buf[16:18], flags)

	return buf
}

func TestParseGPS_NorthEast(t *testing.T) {
	ts := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	content := buildGPSContent(ts, 30.0, 31.5, 60, 90, 8, true, true, true, true)
	raw := buildRawPacket(protocol.ProtocolGPS, content, 10)

	pkt, err := protocol.ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket error: %v", err)
	}

	if pkt.Type != protocol.PacketTypeGPS {
		t.Errorf("expected PacketTypeGPS, got %v", pkt.Type)
	}
	gps := pkt.GPS
	if gps == nil {
		t.Fatal("GPS field is nil")
	}
	if !gps.Timestamp.Equal(ts) {
		t.Errorf("Timestamp: got %v, want %v", gps.Timestamp, ts)
	}
	if !approxEqual(gps.Latitude, 30.0, 0.001) {
		t.Errorf("Latitude: got %f, want ~30.0", gps.Latitude)
	}
	if !approxEqual(gps.Longitude, 31.5, 0.001) {
		t.Errorf("Longitude: got %f, want ~31.5", gps.Longitude)
	}
	if gps.Speed != 60 {
		t.Errorf("Speed: got %d, want 60", gps.Speed)
	}
	if gps.Course != 90 {
		t.Errorf("Course: got %d, want 90", gps.Course)
	}
	if gps.Satellites != 8 {
		t.Errorf("Satellites: got %d, want 8", gps.Satellites)
	}
	if !gps.GPSValid {
		t.Error("GPSValid should be true")
	}
	if !gps.Realtime {
		t.Error("Realtime should be true")
	}
}

func TestParseGPS_SouthWest(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	content := buildGPSContent(ts, 10.0, 20.0, 0, 270, 4, true, false, false, false)
	raw := buildRawPacket(protocol.ProtocolGPS, content, 11)

	pkt, err := protocol.ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket error: %v", err)
	}
	gps := pkt.GPS
	if gps == nil {
		t.Fatal("GPS field is nil")
	}
	if gps.Latitude >= 0 {
		t.Errorf("expected negative latitude (South), got %f", gps.Latitude)
	}
	if gps.Longitude >= 0 {
		t.Errorf("expected negative longitude (West), got %f", gps.Longitude)
	}
}

func TestParseGPS_TooShort(t *testing.T) {
	raw := buildRawPacket(protocol.ProtocolGPS, []byte{0x01, 0x02}, 12)
	_, err := protocol.ParsePacket(raw)
	if err == nil {
		t.Fatal("expected error for truncated GPS content")
	}
}

// --- Alarm packet tests ---

func TestParseAlarm(t *testing.T) {
	ts := time.Date(2024, 3, 20, 8, 0, 0, 0, time.UTC)
	gpsContent := buildGPSContent(ts, 25.0, 55.0, 10, 45, 6, true, true, true, true)
	// Alarm content = [alarm_type(1)] + GPS content
	alarmContent := append([]byte{0x09}, gpsContent...) // 0x09 = over-speed alarm
	raw := buildRawPacket(protocol.ProtocolAlarm, alarmContent, 20)

	pkt, err := protocol.ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket error: %v", err)
	}
	if pkt.Type != protocol.PacketTypeGPSAlarm {
		t.Errorf("expected PacketTypeGPSAlarm, got %v", pkt.Type)
	}
	if pkt.GPS == nil {
		t.Fatal("GPS field is nil")
	}
	if pkt.GPS.AlarmType != 0x09 {
		t.Errorf("AlarmType: got 0x%02X, want 0x09", pkt.GPS.AlarmType)
	}
}

// --- BuildResponse tests ---

func TestBuildResponse(t *testing.T) {
	resp := protocol.BuildResponse(protocol.ProtocolLogin, 42)

	if len(resp) != 10 {
		t.Fatalf("response length: got %d, want 10", len(resp))
	}
	if resp[0] != protocol.StartByte1 || resp[1] != protocol.StartByte1 {
		t.Error("response missing start bytes")
	}
	if resp[8] != protocol.EndByte1 || resp[9] != protocol.EndByte2 {
		t.Error("response missing end bytes")
	}
	if resp[2] != 0x05 {
		t.Errorf("response length field: got 0x%02X, want 0x05", resp[2])
	}
	if resp[3] != protocol.ProtocolLogin {
		t.Errorf("response protocol: got 0x%02X, want 0x%02X", resp[3], protocol.ProtocolLogin)
	}
	sn := binary.BigEndian.Uint16(resp[4:6])
	if sn != 42 {
		t.Errorf("response serial: got %d, want 42", sn)
	}
}

// --- CRC / bad-packet tests ---

func TestBadStartBytes(t *testing.T) {
	raw := []byte{0xAA, 0xBB, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00, 0x0D, 0x0A}
	_, err := protocol.ParsePacket(raw)
	if err == nil {
		t.Fatal("expected error for bad start bytes")
	}
}

func TestBadCRC(t *testing.T) {
	raw := buildRawPacket(protocol.ProtocolLogin, []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12, 0x34, 0x5F}, 1)
	// Corrupt the CRC bytes.
	raw[len(raw)-4] ^= 0xFF
	_, err := protocol.ParsePacket(raw)
	if err == nil {
		t.Fatal("expected CRC mismatch error")
	}
}

func TestPacketTooShort(t *testing.T) {
	_, err := protocol.ParsePacket([]byte{0x78, 0x78})
	if err == nil {
		t.Fatal("expected error for too-short packet")
	}
}

func TestUnknownProtocol(t *testing.T) {
	raw := buildRawPacket(0xFF, []byte{0xDE, 0xAD}, 99)
	pkt, err := protocol.ParsePacket(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pkt.Type != protocol.PacketTypeUnknown {
		t.Errorf("expected PacketTypeUnknown, got %v", pkt.Type)
	}
}

// --- helpers ---

func approxEqual(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}
