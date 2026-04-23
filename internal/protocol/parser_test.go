package protocol_test

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── Frame builder helpers ────────────────────────────────────────────────────

const testIMEI = "123456789012345"

// buildFrame assembles a complete, valid iStartek frame from the given components.
func buildFrame(deviceID string, proto byte, content []byte, serialNo uint16) []byte {
	// Length = DeviceID(15) + Protocol(1) + Content(N) + Serial(2) + Checksum(2)
	lengthVal := uint16(15 + 1 + len(content) + 2 + 2)

	// Allocate: Start(2) + Length(2) + DeviceID(15) + Protocol(1) + Content + Serial(2) + CRC(2) + End(2)
	totalLen := 2 + 2 + 15 + 1 + len(content) + 2 + 2 + 2
	frame := make([]byte, totalLen)

	frame[0] = 0x24
	frame[1] = 0x24
	binary.BigEndian.PutUint16(frame[2:4], lengthVal)
	copy(frame[4:19], padDeviceID(deviceID))
	frame[19] = proto
	copy(frame[20:20+len(content)], content)
	off := 20 + len(content)
	binary.BigEndian.PutUint16(frame[off:off+2], serialNo)
	// Checksum over bytes 2 … off+2 (Length through SerialNo)
	crc := protocol.BuildChecksum(frame[2 : off+2])
	binary.BigEndian.PutUint16(frame[off+2:off+4], crc)
	frame[off+4] = 0x0D
	frame[off+5] = 0x0A
	return frame
}

func padDeviceID(id string) []byte {
	b := make([]byte, 15)
	copy(b, id)
	return b
}

// buildTimeBytes encodes a time as the 6-byte YY MM DD HH MM SS representation.
func buildTimeBytes(t time.Time) []byte {
	return []byte{
		byte(t.Year() - 2000),
		byte(t.Month()),
		byte(t.Day()),
		byte(t.Hour()),
		byte(t.Minute()),
		byte(t.Second()),
	}
}

// ─── ParseFrame tests ─────────────────────────────────────────────────────────

func TestParseFrame_ValidHeartbeat(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 1)
	pkt, err := protocol.ParseFrame(frame)
	require.NoError(t, err)
	assert.Equal(t, testIMEI, pkt.DeviceID)
	assert.Equal(t, byte(protocol.ProtocolHeartbeat), pkt.Protocol)
	assert.Equal(t, uint16(1), pkt.SerialNo)
	assert.Empty(t, pkt.Content)
}

func TestParseFrame_TooShort(t *testing.T) {
	_, err := protocol.ParseFrame([]byte{0x24, 0x24, 0x00})
	assert.ErrorIs(t, err, protocol.ErrShortFrame)
}

func TestParseFrame_BadStartMarker(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 1)
	frame[0] = 0x00
	_, err := protocol.ParseFrame(frame)
	assert.ErrorIs(t, err, protocol.ErrBadStartMarker)
}

func TestParseFrame_BadEndMarker(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 1)
	frame[len(frame)-1] = 0x00
	_, err := protocol.ParseFrame(frame)
	assert.ErrorIs(t, err, protocol.ErrBadEndMarker)
}

func TestParseFrame_LengthMismatch(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 1)
	// Corrupt the length field
	binary.BigEndian.PutUint16(frame[2:4], 999)
	_, err := protocol.ParseFrame(frame)
	assert.ErrorIs(t, err, protocol.ErrLengthMismatch)
}

func TestParseFrame_BadChecksum(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 1)
	// Flip a checksum byte
	frame[len(frame)-4] ^= 0xFF
	_, err := protocol.ParseFrame(frame)
	assert.ErrorIs(t, err, protocol.ErrBadChecksum)
}

// ─── DecodeLogin tests ────────────────────────────────────────────────────────

func makeLoginContent(ts time.Time) []byte {
	c := make([]byte, 17)
	copy(c[0:6], buildTimeBytes(ts))
	binary.BigEndian.PutUint16(c[6:8], 460)  // MCC
	c[8] = 1                                  // MNC
	binary.BigEndian.PutUint16(c[9:11], 100) // LAC
	// CellID: 3 bytes
	c[11] = 0x00
	c[12] = 0x01
	c[13] = 0xAB
	c[14] = 20  // Signal
	c[15] = 0x00 // Language high byte
	c[16] = 0x01 // Language low byte (Chinese)
	return c
}

func TestDecodeLogin_Valid(t *testing.T) {
	ts := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	content := makeLoginContent(ts)
	frame := buildFrame(testIMEI, protocol.ProtocolLogin, content, 1)

	raw, err := protocol.ParseFrame(frame)
	require.NoError(t, err)

	login, err := protocol.DecodeLogin(raw)
	require.NoError(t, err)

	assert.Equal(t, testIMEI, login.DeviceID)
	assert.Equal(t, ts, login.Time)
	assert.Equal(t, uint16(460), login.MCC)
	assert.Equal(t, uint8(1), login.MNC)
	assert.Equal(t, uint16(100), login.LAC)
	assert.Equal(t, uint32(0x0001AB), login.CellID)
	assert.Equal(t, uint8(20), login.Signal)
}

func TestDecodeLogin_WrongProtocol(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 1)
	raw, err := protocol.ParseFrame(frame)
	require.NoError(t, err)

	_, err = protocol.DecodeLogin(raw)
	assert.Error(t, err)
}

func TestDecodeLogin_ContentTooShort(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolLogin, []byte{0x01, 0x02}, 1)
	raw, err := protocol.ParseFrame(frame)
	require.NoError(t, err)

	_, err = protocol.DecodeLogin(raw)
	assert.ErrorIs(t, err, protocol.ErrContentTooShort)
}

// ─── DecodeHeartbeat tests ────────────────────────────────────────────────────

func TestDecodeHeartbeat_Valid(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 42)
	raw, err := protocol.ParseFrame(frame)
	require.NoError(t, err)

	hb, err := protocol.DecodeHeartbeat(raw)
	require.NoError(t, err)
	assert.Equal(t, testIMEI, hb.DeviceID)
	assert.Equal(t, uint16(42), hb.SerialNo)
}

func TestDecodeHeartbeat_WrongProtocol(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolLogin, makeLoginContent(time.Now()), 1)
	raw, err := protocol.ParseFrame(frame)
	require.NoError(t, err)

	_, err = protocol.DecodeHeartbeat(raw)
	assert.Error(t, err)
}

// ─── DecodeGPS tests ──────────────────────────────────────────────────────────

func makeGPSContent(ts time.Time, lat, lon float64, speed uint8) []byte {
	c := make([]byte, 44)
	copy(c[0:6], buildTimeBytes(ts))
	c[6] = 34  // GPS info length
	c[7] = 0   // GPS accuracy flags
	c[8] = speed

	heading := uint16(90)
	binary.BigEndian.PutUint16(c[9:11], heading)

	altitude := int16(50)
	binary.BigEndian.PutUint16(c[11:13], uint16(altitude))

	lonInt := int32(lon * 1e6)
	latInt := int32(lat * 1e6)
	binary.BigEndian.PutUint32(c[13:17], uint32(lonInt))
	binary.BigEndian.PutUint32(c[17:21], uint32(latInt))

	// GPS date bytes (DDMMYY) at c[21:25]
	c[21] = byte(ts.Day())
	c[22] = byte(ts.Month())
	c[23] = byte(ts.Year() - 2000)
	c[24] = 0

	c[25] = 8  // satellites
	c[26] = 20 // RSSI

	ioStatus := uint16(0x0007) // ignition | ACC | GPSTracking
	binary.BigEndian.PutUint16(c[27:29], ioStatus)

	binary.BigEndian.PutUint16(c[29:31], 12000) // analog0 = 12V
	binary.BigEndian.PutUint16(c[31:33], 4100)  // analog1 = 4.1V

	// Odometer: 3 bytes = 1000 meters
	c[33] = 0x00
	c[34] = 0x03
	c[35] = 0xE8

	binary.BigEndian.PutUint16(c[36:38], 460) // MCC
	c[38] = 1                                  // MNC
	binary.BigEndian.PutUint16(c[39:41], 100) // LAC
	c[41] = 0x00
	c[42] = 0x01
	c[43] = 0xAB // CellID
	return c
}

func TestDecodeGPS_Valid(t *testing.T) {
	ts := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	lat, lon := 30.12345, 31.67890
	content := makeGPSContent(ts, lat, lon, 60)
	frame := buildFrame(testIMEI, protocol.ProtocolGPSData, content, 10)

	raw, err := protocol.ParseFrame(frame)
	require.NoError(t, err)

	gps, err := protocol.DecodeGPS(raw)
	require.NoError(t, err)

	assert.Equal(t, testIMEI, gps.DeviceID)
	assert.Equal(t, ts, gps.Time)
	assert.InDelta(t, lat, gps.Latitude, 1e-6)
	assert.InDelta(t, lon, gps.Longitude, 1e-6)
	assert.Equal(t, uint8(60), gps.Speed)
	assert.Equal(t, uint16(90), gps.Heading)
	assert.Equal(t, int16(50), gps.Altitude)
	assert.Equal(t, uint8(8), gps.Satellites)
	assert.Equal(t, uint8(20), gps.RSSI)
	assert.Equal(t, uint16(12000), gps.Analog0)
	assert.Equal(t, uint32(1000), gps.Odometer)
	assert.True(t, gps.Status.Ignition)
	assert.True(t, gps.Status.ACC)
	assert.True(t, gps.Status.GPSTracking)
	assert.False(t, gps.Status.RelayStatus)
}

func TestDecodeGPS_ContentTooShort(t *testing.T) {
	frame := buildFrame(testIMEI, protocol.ProtocolGPSData, make([]byte, 10), 1)
	raw, err := protocol.ParseFrame(frame)
	require.NoError(t, err)

	_, err = protocol.DecodeGPS(raw)
	assert.ErrorIs(t, err, protocol.ErrContentTooShort)
}

// ─── DecodeAlarm tests ────────────────────────────────────────────────────────

func makeAlarmContent(ts time.Time, alarmType protocol.AlarmType, lat, lon float64) []byte {
	c := make([]byte, 28)
	c[0] = byte(alarmType)
	copy(c[1:7], buildTimeBytes(ts))
	c[7] = 20 // GPS info length
	c[8] = 0  // GPS accuracy
	c[9] = 0  // speed

	binary.BigEndian.PutUint16(c[10:12], 0)   // heading
	binary.BigEndian.PutUint16(c[12:14], 100) // altitude

	lonInt := int32(lon * 1e6)
	latInt := int32(lat * 1e6)
	binary.BigEndian.PutUint32(c[14:18], uint32(lonInt))
	binary.BigEndian.PutUint32(c[18:22], uint32(latInt))

	// GPS date at c[22:26]
	c[22] = byte(ts.Day())
	c[23] = byte(ts.Month())
	c[24] = byte(ts.Year() - 2000)
	c[25] = 0
	c[26] = 6  // satellites
	c[27] = 15 // RSSI
	return c
}

func TestDecodeAlarm_SOS(t *testing.T) {
	ts := time.Date(2024, 1, 20, 12, 0, 0, 0, time.UTC)
	lat, lon := 25.0, 45.0
	content := makeAlarmContent(ts, protocol.AlarmSOS, lat, lon)
	frame := buildFrame(testIMEI, protocol.ProtocolAlarm, content, 5)

	raw, err := protocol.ParseFrame(frame)
	require.NoError(t, err)

	alarm, err := protocol.DecodeAlarm(raw)
	require.NoError(t, err)

	assert.Equal(t, protocol.AlarmSOS, alarm.Alarm)
	assert.Equal(t, testIMEI, alarm.DeviceID)
	assert.Equal(t, ts, alarm.Time)
	assert.InDelta(t, lat, alarm.Latitude, 1e-6)
	assert.InDelta(t, lon, alarm.Longitude, 1e-6)
	assert.Equal(t, uint8(6), alarm.Satellites)
}

// ─── Response builder tests ───────────────────────────────────────────────────

func TestBuildLoginResponse(t *testing.T) {
	resp := protocol.BuildLoginResponse(1)
	// Must start and end correctly
	assert.Equal(t, byte(0x24), resp[0])
	assert.Equal(t, byte(0x24), resp[1])
	assert.Equal(t, byte(0x0D), resp[len(resp)-2])
	assert.Equal(t, byte(0x0A), resp[len(resp)-1])
	assert.Equal(t, byte(protocol.ProtocolLogin), resp[19])
}

func TestBuildHeartbeatResponse(t *testing.T) {
	resp := protocol.BuildHeartbeatResponse(99)
	assert.Equal(t, byte(protocol.ProtocolHeartbeat), resp[19])
	// serial number at offset 20–21
	serial := binary.BigEndian.Uint16(resp[20:22])
	assert.Equal(t, uint16(99), serial)
}

func TestBuildGPSResponse(t *testing.T) {
	resp := protocol.BuildGPSResponse(7)
	assert.Len(t, resp, 26)
}

// ─── Checksum tests ───────────────────────────────────────────────────────────

func TestChecksum_Symmetry(t *testing.T) {
	data := []byte{0x10, 0x20, 0x30, 0x40}
	crc := protocol.BuildChecksum(data)
	// High byte == Low byte (XOR result duplicated)
	assert.Equal(t, byte(crc>>8), byte(crc&0xFF))
}

func TestChecksum_Empty(t *testing.T) {
	crc := protocol.BuildChecksum([]byte{})
	assert.Equal(t, uint16(0), crc)
}
