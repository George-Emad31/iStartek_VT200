// Package protocol implements the iStartek GPS Tracker Communication Protocol v2.3.
//
// Packet frame layout (all multi-byte fields are big-endian):
//
//	┌────────┬────────┬─────────────────┬──────────┬─────────────────────┬──────────┬──────────┬────────┐
//	│ Start  │ Length │   Device ID     │ Protocol │    Information      │ Serial # │ Checksum │  End   │
//	│ 2 bytes│ 2 bytes│   15 bytes      │  1 byte  │    N bytes          │  2 bytes │  2 bytes │ 2 bytes│
//	│  0x2424│        │ (IMEI, ASCII)   │          │                     │          │ (XOR)    │ 0x0D0A │
//	└────────┴────────┴─────────────────┴──────────┴─────────────────────┴──────────┴──────────┴────────┘
//
// The Length field covers everything from DeviceID to the end of Checksum.
// The Checksum (2 bytes) is the XOR of all bytes from the Length field through
// the Serial Number field (inclusive).
package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// ─── Constants ────────────────────────────────────────────────────────────────

const (
	startByte1 = 0x24 // '$'
	startByte2 = 0x24 // '$'
	endByte1   = 0x0D // '\r'
	endByte2   = 0x0A // '\n'

	// Minimum frame size: Start(2)+Length(2)+DeviceID(15)+Protocol(1)+Serial(2)+Checksum(2)+End(2) = 26
	minFrameLen = 26

	// Fixed header region lengths
	deviceIDLen = 15
	headerLen   = 2 + 2 + deviceIDLen + 1 // Start + Length + DeviceID + Protocol
)

// Protocol numbers as defined in Communication Protocol v2.3.
const (
	ProtocolLogin     = 0x01 // Device login / registration
	ProtocolHeartbeat = 0x08 // Keep-alive heartbeat
	ProtocolGPSData   = 0x22 // GPS + LBS + Status data
	ProtocolAlarm     = 0x26 // Alarm data packet
)

// AlarmType enumerates alarm codes used in Alarm packets.
type AlarmType byte

const (
	AlarmSOS             AlarmType = 0x01
	AlarmPowerCut        AlarmType = 0x02
	AlarmVibration       AlarmType = 0x03
	AlarmEnterGeoFence   AlarmType = 0x04
	AlarmExitGeoFence    AlarmType = 0x05
	AlarmOverSpeed       AlarmType = 0x06
	AlarmMovement        AlarmType = 0x09
	AlarmLowBattery      AlarmType = 0x0B
	AlarmExternalPowerOff AlarmType = 0x0D
)

// ─── Packet types ─────────────────────────────────────────────────────────────

// RawPacket contains the decoded frame before interpretation.
type RawPacket struct {
	DeviceID   string // 15-char IMEI string
	Protocol   byte
	Content    []byte // raw information content (without serial / checksum / end)
	SerialNo   uint16
	Checksum   uint16
}

// LoginPacket represents a device login (protocol 0x01).
type LoginPacket struct {
	DeviceID string
	SerialNo uint16
	Time     time.Time
	// Cell tower info
	MCC    uint16
	MNC    uint8
	LAC    uint16
	CellID uint32 // 3-byte cell ID
	Signal uint8
}

// HeartbeatPacket represents a heartbeat / keep-alive (protocol 0x08).
type HeartbeatPacket struct {
	DeviceID string
	SerialNo uint16
}

// GPSStatus holds the I/O and vehicle status flags from a GPS data packet.
type GPSStatus struct {
	Ignition    bool
	ACC         bool
	GPSTracking bool
	RelayStatus bool
	OilElec     bool
}

// GPSPacket represents a GPS + LBS + Status data packet (protocol 0x22).
type GPSPacket struct {
	DeviceID  string
	SerialNo  uint16
	Time      time.Time
	Latitude  float64 // degrees, positive = North
	Longitude float64 // degrees, positive = East
	Speed     uint8   // km/h
	Heading   uint16  // degrees (0–359)
	Altitude  int16   // meters
	Satellites uint8
	RSSI      uint8
	// LBS
	MCC    uint16
	MNC    uint8
	LAC    uint16
	CellID uint32
	// Status
	Status    GPSStatus
	IOStatus  uint16
	Analog0   uint16 // mV
	Analog1   uint16 // mV
	Odometer  uint32 // meters (3-byte field)
}

// AlarmPacket represents an alarm data packet (protocol 0x26).
type AlarmPacket struct {
	DeviceID  string
	SerialNo  uint16
	Time      time.Time
	Alarm     AlarmType
	Latitude  float64
	Longitude float64
	Speed     uint8
	Heading   uint16
	Altitude  int16
	Satellites uint8
	RSSI      uint8
}

// ─── Errors ───────────────────────────────────────────────────────────────────

var (
	ErrShortFrame     = errors.New("protocol: frame too short")
	ErrBadStartMarker = errors.New("protocol: invalid start marker")
	ErrBadEndMarker   = errors.New("protocol: invalid end marker")
	ErrLengthMismatch = errors.New("protocol: length field mismatch")
	ErrBadChecksum    = errors.New("protocol: checksum mismatch")
	ErrUnknownProto   = errors.New("protocol: unknown protocol number")
	ErrContentTooShort = errors.New("protocol: content too short for protocol")
)

// ─── Frame parsing ────────────────────────────────────────────────────────────

// ParseFrame validates a raw byte buffer and returns a RawPacket.
// The buffer must contain exactly one complete frame (Start … End).
func ParseFrame(buf []byte) (*RawPacket, error) {
	if len(buf) < minFrameLen {
		return nil, ErrShortFrame
	}

	// Validate start marker
	if buf[0] != startByte1 || buf[1] != startByte2 {
		return nil, ErrBadStartMarker
	}

	// Validate end marker
	last := len(buf) - 1
	if buf[last] != endByte2 || buf[last-1] != endByte1 {
		return nil, ErrBadEndMarker
	}

	// Length field (bytes 2–3): covers DeviceID through Checksum
	lengthField := binary.BigEndian.Uint16(buf[2:4])
	// Frame = Start(2) + Length(2) + <lengthField bytes> + End(2)
	expectedTotal := int(lengthField) + 6
	if len(buf) != expectedTotal {
		return nil, ErrLengthMismatch
	}

	// Extract device ID (bytes 4–18)
	deviceID := string(buf[4:19])

	// Protocol number (byte 19)
	proto := buf[19]

	// Content: bytes 20 … (last-5), i.e. everything before Serial(2)+Checksum(2)+End(2)
	contentEnd := len(buf) - 6 // exclusive; 4 trailer bytes before \r\n
	var content []byte
	if contentEnd > 20 {
		content = buf[20:contentEnd]
	}

	// Serial number (2 bytes before checksum)
	serialNo := binary.BigEndian.Uint16(buf[contentEnd : contentEnd+2])

	// Stored checksum
	storedCRC := binary.BigEndian.Uint16(buf[contentEnd+2 : contentEnd+4])

	// Verify checksum: XOR of bytes from Length field (offset 2) through SerialNo (inclusive)
	calcCRC := checksum(buf[2 : contentEnd+2])
	if calcCRC != storedCRC {
		return nil, fmt.Errorf("%w: got 0x%04X want 0x%04X", ErrBadChecksum, storedCRC, calcCRC)
	}

	return &RawPacket{
		DeviceID: deviceID,
		Protocol: proto,
		Content:  content,
		SerialNo: serialNo,
		Checksum: storedCRC,
	}, nil
}

// checksum computes the XOR-based checksum over the given bytes.
// The result is zero-extended to a uint16 (high byte = 0x00 XOR sum, low byte = XOR sum).
// The protocol uses the same XOR value in both bytes of the 2-byte checksum field.
func checksum(data []byte) uint16 {
	var xor byte
	for _, b := range data {
		xor ^= b
	}
	return uint16(xor)<<8 | uint16(xor)
}

// BuildChecksum is exported for use by tests and frame builders.
func BuildChecksum(data []byte) uint16 {
	return checksum(data)
}

// ─── Protocol-specific decoders ───────────────────────────────────────────────

// DecodeLogin decodes a LoginPacket from a RawPacket.
// Login content layout (17 bytes):
//
//	Time(6) + MCC(2) + MNC(1) + LAC(2) + CellID(3) + Signal(1) + Language(2)
func DecodeLogin(pkt *RawPacket) (*LoginPacket, error) {
	if pkt.Protocol != ProtocolLogin {
		return nil, fmt.Errorf("protocol: expected 0x%02X got 0x%02X", ProtocolLogin, pkt.Protocol)
	}
	if len(pkt.Content) < 17 {
		return nil, ErrContentTooShort
	}
	c := pkt.Content
	t, err := decodeTime(c[0:6])
	if err != nil {
		return nil, err
	}
	cellID := uint32(c[11])<<16 | uint32(c[12])<<8 | uint32(c[13])
	return &LoginPacket{
		DeviceID: pkt.DeviceID,
		SerialNo: pkt.SerialNo,
		Time:     t,
		MCC:      binary.BigEndian.Uint16(c[6:8]),
		MNC:      c[8],
		LAC:      binary.BigEndian.Uint16(c[9:11]),
		CellID:   cellID,
		Signal:   c[14],
	}, nil
}

// DecodeHeartbeat decodes a HeartbeatPacket from a RawPacket.
func DecodeHeartbeat(pkt *RawPacket) (*HeartbeatPacket, error) {
	if pkt.Protocol != ProtocolHeartbeat {
		return nil, fmt.Errorf("protocol: expected 0x%02X got 0x%02X", ProtocolHeartbeat, pkt.Protocol)
	}
	return &HeartbeatPacket{
		DeviceID: pkt.DeviceID,
		SerialNo: pkt.SerialNo,
	}, nil
}

// DecodeGPS decodes a GPSPacket from a RawPacket.
// GPS content layout (minimum 40 bytes):
//
//	Time(6) + GPSInfoLen(1) + GPSAccuracy(1) + Speed(1) + Heading(2) + Altitude(2) +
//	Longitude(4) + Latitude(4) + DateTime(4) + Satellites(1) + RSSI(1) + IOStatus(2) +
//	Analog0(2) + Analog1(2) + Odometer(3) + MCC(2) + MNC(1) + LAC(2) + CellID(3)
func DecodeGPS(pkt *RawPacket) (*GPSPacket, error) {
	if pkt.Protocol != ProtocolGPSData {
		return nil, fmt.Errorf("protocol: expected 0x%02X got 0x%02X", ProtocolGPSData, pkt.Protocol)
	}
	if len(pkt.Content) < 40 {
		return nil, ErrContentTooShort
	}
	c := pkt.Content
	t, err := decodeTime(c[0:6])
	if err != nil {
		return nil, err
	}

	// GPS info block starts at offset 6
	// c[6]  = GPS info length (covers bytes from offset 7 onward in GPS block)
	// c[7]  = GPS accuracy / fix flags
	speed := c[8]
	heading := binary.BigEndian.Uint16(c[9:11])
	altitude := int16(binary.BigEndian.Uint16(c[11:13]))

	// Longitude and latitude: stored as int32, unit = 1e-6 degrees
	lon := int32(binary.BigEndian.Uint32(c[13:17]))
	lat := int32(binary.BigEndian.Uint32(c[17:21]))

	// c[21:25] = GPS date (DDMMYY packed – we already decoded date from server time)
	satellites := c[25]
	rssi := c[26]

	ioStatus := binary.BigEndian.Uint16(c[27:29])
	analog0 := binary.BigEndian.Uint16(c[29:31])
	analog1 := binary.BigEndian.Uint16(c[31:33])
	odometer := uint32(c[33])<<16 | uint32(c[34])<<8 | uint32(c[35])

	mcc := binary.BigEndian.Uint16(c[36:38])
	mnc := c[38]
	lac := binary.BigEndian.Uint16(c[39:41])
	var cellID uint32
	if len(c) >= 44 {
		cellID = uint32(c[41])<<16 | uint32(c[42])<<8 | uint32(c[43])
	}

	status := decodeGPSStatus(ioStatus)

	return &GPSPacket{
		DeviceID:   pkt.DeviceID,
		SerialNo:   pkt.SerialNo,
		Time:       t,
		Latitude:   float64(lat) / 1e6,
		Longitude:  float64(lon) / 1e6,
		Speed:      speed,
		Heading:    heading,
		Altitude:   altitude,
		Satellites: satellites,
		RSSI:       rssi,
		IOStatus:   ioStatus,
		Analog0:    analog0,
		Analog1:    analog1,
		Odometer:   odometer,
		MCC:        mcc,
		MNC:        mnc,
		LAC:        lac,
		CellID:     cellID,
		Status:     status,
	}, nil
}

// DecodeAlarm decodes an AlarmPacket from a RawPacket.
// Alarm content layout: AlarmType(1) + GPS block identical to GPSPacket offset 0–25.
func DecodeAlarm(pkt *RawPacket) (*AlarmPacket, error) {
	if pkt.Protocol != ProtocolAlarm {
		return nil, fmt.Errorf("protocol: expected 0x%02X got 0x%02X", ProtocolAlarm, pkt.Protocol)
	}
	if len(pkt.Content) < 27 {
		return nil, ErrContentTooShort
	}
	c := pkt.Content
	alarmType := AlarmType(c[0])
	// GPS data follows the alarm type byte
	t, err := decodeTime(c[1:7])
	if err != nil {
		return nil, err
	}
	speed := c[9]
	heading := binary.BigEndian.Uint16(c[10:12])
	altitude := int16(binary.BigEndian.Uint16(c[12:14]))
	lon := int32(binary.BigEndian.Uint32(c[14:18]))
	lat := int32(binary.BigEndian.Uint32(c[18:22]))
	satellites := c[26]
	var rssi uint8
	if len(c) > 27 {
		rssi = c[27]
	}

	return &AlarmPacket{
		DeviceID:   pkt.DeviceID,
		SerialNo:   pkt.SerialNo,
		Time:       t,
		Alarm:      alarmType,
		Latitude:   float64(lat) / 1e6,
		Longitude:  float64(lon) / 1e6,
		Speed:      speed,
		Heading:    heading,
		Altitude:   altitude,
		Satellites: satellites,
		RSSI:       rssi,
	}, nil
}

// ─── Response builders ────────────────────────────────────────────────────────

// BuildLoginResponse creates the server response frame for a login packet.
// The server echoes the Serial Number back to the device.
func BuildLoginResponse(serialNo uint16) []byte {
	return buildResponse(ProtocolLogin, serialNo)
}

// BuildHeartbeatResponse creates the server response frame for a heartbeat packet.
func BuildHeartbeatResponse(serialNo uint16) []byte {
	return buildResponse(ProtocolHeartbeat, serialNo)
}

// BuildGPSResponse creates the server response frame for a GPS data packet.
func BuildGPSResponse(serialNo uint16) []byte {
	return buildResponse(ProtocolGPSData, serialNo)
}

// buildResponse constructs a minimal server-to-device response frame.
// The response format from the server uses the same framing as device packets.
//
//	Layout: Start(2) + Length(2) + "server          "(15) + Protocol(1) + Serial(2) + Checksum(2) + End(2)
//
// Total = 26 bytes (no content / information section).
func buildResponse(proto byte, serialNo uint16) []byte {
	frame := make([]byte, 26)
	frame[0] = startByte1
	frame[1] = startByte2
	// Length = DeviceID(15) + Protocol(1) + Serial(2) + Checksum(2) = 20
	binary.BigEndian.PutUint16(frame[2:4], 20)
	// Server identifier (15 bytes, space-padded)
	copy(frame[4:19], "server         ")
	frame[19] = proto
	binary.BigEndian.PutUint16(frame[20:22], serialNo)
	crc := checksum(frame[2:22])
	binary.BigEndian.PutUint16(frame[22:24], crc)
	frame[24] = endByte1
	frame[25] = endByte2
	return frame
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// decodeTime converts 6 bytes (YY MM DD HH MM SS) to a time.Time in UTC.
func decodeTime(b []byte) (time.Time, error) {
	if len(b) < 6 {
		return time.Time{}, errors.New("protocol: time field too short")
	}
	year := 2000 + int(b[0])
	month := time.Month(b[1])
	day := int(b[2])
	hour := int(b[3])
	min := int(b[4])
	sec := int(b[5])
	return time.Date(year, month, day, hour, min, sec, 0, time.UTC), nil
}

func decodeGPSStatus(ioStatus uint16) GPSStatus {
	return GPSStatus{
		Ignition:    ioStatus&(1<<0) != 0,
		ACC:         ioStatus&(1<<1) != 0,
		GPSTracking: ioStatus&(1<<2) != 0,
		RelayStatus: ioStatus&(1<<3) != 0,
		OilElec:     ioStatus&(1<<4) != 0,
	}
}
