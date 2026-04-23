// Package protocol implements the iStartek VT200 binary communication protocol.
//
// # Packet Layout
//
// Short packet (length fits in 1 byte):
//
//	[0x78][0x78][length][protocol][content…][serial_hi][serial_lo][crc_hi][crc_lo][0x0D][0x0A]
//
// Long packet (length requires 2 bytes):
//
//	[0x79][0x79][len_hi][len_lo][protocol][content…][serial_hi][serial_lo][crc_hi][crc_lo][0x0D][0x0A]
//
// The CRC is computed over bytes from the protocol number through the last byte
// of the serial number, using the CRC-16/IBM (CRC-16-ANSI) algorithm.
package protocol

import "time"

// Protocol numbers (packet types).
const (
	ProtocolLogin     byte = 0x01
	ProtocolGPS       byte = 0x10
	ProtocolStatus    byte = 0x13
	ProtocolString    byte = 0x15
	ProtocolAlarm     byte = 0x16
	ProtocolGPSLBS    byte = 0x12 // GPS + LBS combined
	ProtocolHeartbeat byte = 0x23
	ProtocolGPSLBSExt byte = 0x22 // Extended GPS + LBS
)

// Header start bytes.
const (
	StartByte1  byte = 0x78
	StartByte1L byte = 0x79
	EndByte1    byte = 0x0D
	EndByte2    byte = 0x0A
)

// PacketType classifies a decoded packet.
type PacketType uint8

const (
	PacketTypeUnknown   PacketType = iota
	PacketTypeLogin                // Device authentication / registration
	PacketTypeHeartbeat            // Keep-alive
	PacketTypeGPS                  // Standard GPS location
	PacketTypeGPSAlarm             // GPS location + alarm flag
)

// LoginData contains device identification from a login packet.
type LoginData struct {
	IMEI string // 15-digit IMEI encoded as 8 BCD bytes
}

// GPSData holds a fully parsed location record.
type GPSData struct {
	Timestamp  time.Time
	Latitude   float64 // decimal degrees, positive=North
	Longitude  float64 // decimal degrees, positive=East
	Speed      uint8   // km/h
	Course     uint16  // degrees, 0–359
	Satellites uint8
	GPSValid   bool
	Realtime   bool
	// LBS (cell tower) data
	MCC    uint16
	MNC    uint8
	LAC    uint16
	CellID uint32
	// Alarm (only meaningful for alarm packets)
	AlarmType uint8
	Voltage   uint8  // device voltage level (0–6)
	Signal    uint8  // GSM signal strength (0–4)
}

// Packet is the result of parsing a single raw VT200 frame.
type Packet struct {
	Type       PacketType
	Protocol   byte
	SerialNo   uint16
	Login      *LoginData
	GPS        *GPSData
	RawPayload []byte // unparsed content bytes
}
