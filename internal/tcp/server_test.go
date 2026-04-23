package tcp_test

import (
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
	tcppkg "github.com/George-Emad31/iStartek_VT200/internal/tcp"
)

// ─── Mock handler ─────────────────────────────────────────────────────────────

type recordingHandler struct {
	mu         sync.Mutex
	logins     []*protocol.LoginPacket
	heartbeats []*protocol.HeartbeatPacket
	gpsPackets []*protocol.GPSPacket
	alarms     []*protocol.AlarmPacket
}

func (h *recordingHandler) OnLogin(_ net.Conn, pkt *protocol.LoginPacket) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logins = append(h.logins, pkt)
}

func (h *recordingHandler) OnHeartbeat(_ net.Conn, pkt *protocol.HeartbeatPacket) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.heartbeats = append(h.heartbeats, pkt)
}

func (h *recordingHandler) OnGPS(_ net.Conn, pkt *protocol.GPSPacket) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gpsPackets = append(h.gpsPackets, pkt)
}

func (h *recordingHandler) OnAlarm(_ net.Conn, pkt *protocol.AlarmPacket) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.alarms = append(h.alarms, pkt)
}

// ─── Frame builder (mirrors the one in protocol tests) ────────────────────────

const testIMEI = "123456789012345"

func buildFrame(deviceID string, proto byte, content []byte, serialNo uint16) []byte {
	lengthVal := uint16(15 + 1 + len(content) + 2 + 2)
	totalLen := 2 + 2 + 15 + 1 + len(content) + 2 + 2 + 2
	frame := make([]byte, totalLen)
	frame[0] = 0x24
	frame[1] = 0x24
	binary.BigEndian.PutUint16(frame[2:4], lengthVal)
	padded := make([]byte, 15)
	copy(padded, deviceID)
	copy(frame[4:19], padded)
	frame[19] = proto
	copy(frame[20:20+len(content)], content)
	off := 20 + len(content)
	binary.BigEndian.PutUint16(frame[off:off+2], serialNo)
	crc := protocol.BuildChecksum(frame[2 : off+2])
	binary.BigEndian.PutUint16(frame[off+2:off+4], crc)
	frame[off+4] = 0x0D
	frame[off+5] = 0x0A
	return frame
}

func makeLoginContent() []byte {
	c := make([]byte, 17)
	ts := time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)
	c[0] = byte(ts.Year() - 2000)
	c[1] = byte(ts.Month())
	c[2] = byte(ts.Day())
	c[3] = byte(ts.Hour())
	c[4] = byte(ts.Minute())
	c[5] = byte(ts.Second())
	binary.BigEndian.PutUint16(c[6:8], 460)
	c[8] = 1
	binary.BigEndian.PutUint16(c[9:11], 100)
	c[11] = 0x00
	c[12] = 0x01
	c[13] = 0xAB
	c[14] = 20
	c[15] = 0x00
	c[16] = 0x01
	return c
}

func makeGPSContent() []byte {
	c := make([]byte, 44)
	ts := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	c[0] = byte(ts.Year() - 2000)
	c[1] = byte(ts.Month())
	c[2] = byte(ts.Day())
	c[3] = byte(ts.Hour())
	c[4] = byte(ts.Minute())
	c[5] = byte(ts.Second())
	c[6] = 34
	c[8] = 60 // speed
	binary.BigEndian.PutUint16(c[9:11], 90)
	binary.BigEndian.PutUint16(c[11:13], 50)
	binary.BigEndian.PutUint32(c[13:17], uint32(int32(31.67890*1e6)))
	binary.BigEndian.PutUint32(c[17:21], uint32(int32(30.12345*1e6)))
	c[25] = 8
	c[26] = 20
	binary.BigEndian.PutUint16(c[27:29], 0x0007)
	binary.BigEndian.PutUint16(c[29:31], 12000)
	binary.BigEndian.PutUint16(c[31:33], 4100)
	c[33] = 0x00
	c[34] = 0x03
	c[35] = 0xE8
	binary.BigEndian.PutUint16(c[36:38], 460)
	c[38] = 1
	binary.BigEndian.PutUint16(c[39:41], 100)
	c[41] = 0x00
	c[42] = 0x01
	c[43] = 0xAB
	return c
}

// ─── Helper: start server on a random port ────────────────────────────────────

func startServer(t *testing.T, h *recordingHandler) (*tcppkg.Server, string) {
	t.Helper()
	srv := tcppkg.NewServer("127.0.0.1:0", h)
	require.NoError(t, srv.Start())
	t.Cleanup(func() { srv.Stop() })
	return srv, srv.Addr().String()
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestServer_Login(t *testing.T) {
	h := &recordingHandler{}
	_, addr := startServer(t, h)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	frame := buildFrame(testIMEI, protocol.ProtocolLogin, makeLoginContent(), 1)
	_, err = conn.Write(frame)
	require.NoError(t, err)

	// Read server response (26 bytes)
	resp := make([]byte, 26)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Read(resp)
	require.NoError(t, err)
	assert.Equal(t, byte(0x24), resp[0])
	assert.Equal(t, byte(protocol.ProtocolLogin), resp[19])

	// Give handler goroutine a moment
	time.Sleep(100 * time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.logins, 1)
	assert.Equal(t, testIMEI, h.logins[0].DeviceID)
}

func TestServer_Heartbeat(t *testing.T) {
	h := &recordingHandler{}
	_, addr := startServer(t, h)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	frame := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 2)
	_, err = conn.Write(frame)
	require.NoError(t, err)

	// Read heartbeat response
	resp := make([]byte, 26)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Read(resp)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.ProtocolHeartbeat), resp[19])

	time.Sleep(100 * time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.heartbeats, 1)
	assert.Equal(t, testIMEI, h.heartbeats[0].DeviceID)
}

func TestServer_GPS(t *testing.T) {
	h := &recordingHandler{}
	_, addr := startServer(t, h)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	frame := buildFrame(testIMEI, protocol.ProtocolGPSData, makeGPSContent(), 3)
	_, err = conn.Write(frame)
	require.NoError(t, err)

	// Read GPS response
	resp := make([]byte, 26)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Read(resp)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.ProtocolGPSData), resp[19])

	time.Sleep(100 * time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	require.Len(t, h.gpsPackets, 1)
	assert.Equal(t, testIMEI, h.gpsPackets[0].DeviceID)
	assert.Equal(t, uint8(60), h.gpsPackets[0].Speed)
}

func TestServer_MultipleFramesInOneRead(t *testing.T) {
	h := &recordingHandler{}
	_, addr := startServer(t, h)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Send two heartbeat frames in a single write
	f1 := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 1)
	f2 := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 2)
	combined := append(f1, f2...)
	_, err = conn.Write(combined)
	require.NoError(t, err)

	// Drain responses (2 × 26 bytes = 52)
	resp := make([]byte, 52)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	total := 0
	for total < 52 {
		n, err := conn.Read(resp[total:])
		total += n
		if err != nil {
			break
		}
	}

	time.Sleep(100 * time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	assert.GreaterOrEqual(t, len(h.heartbeats), 2)
}

func TestServer_Stop(t *testing.T) {
	h := &recordingHandler{}
	srv := tcppkg.NewServer("127.0.0.1:0", h)
	require.NoError(t, srv.Start())

	addr := srv.Addr().String()
	srv.Stop()

	// After stop, new connections should be refused
	conn, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if conn != nil {
		conn.Close()
	}
	assert.Error(t, err)
}

func TestServer_CorruptFrame_IsDiscarded(t *testing.T) {
	h := &recordingHandler{}
	_, addr := startServer(t, h)

	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// Send garbage, then a valid heartbeat
	garbage := []byte{0xFF, 0xFF, 0x00, 0x01, 0xAB}
	valid := buildFrame(testIMEI, protocol.ProtocolHeartbeat, nil, 1)
	_, err = conn.Write(append(garbage, valid...))
	require.NoError(t, err)

	resp := make([]byte, 26)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.Read(resp)

	time.Sleep(100 * time.Millisecond)

	h.mu.Lock()
	defer h.mu.Unlock()
	// The valid heartbeat must have been processed
	assert.GreaterOrEqual(t, len(h.heartbeats), 1)
}
