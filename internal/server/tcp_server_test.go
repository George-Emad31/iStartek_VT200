package server_test

import (
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
	"github.com/George-Emad31/iStartek_VT200/internal/server"
)

// buildRawPacket is a test helper that assembles a valid VT200 short frame.
func buildRawPacket(proto byte, content []byte, serialNo uint16) []byte {
	bodyLen := 1 + len(content) + 2 + 2
	buf := make([]byte, 0, 3+bodyLen+2)
	buf = append(buf, protocol.StartByte1, protocol.StartByte1, byte(bodyLen))
	buf = append(buf, proto)
	buf = append(buf, content...)

	sn := [2]byte{}
	binary.BigEndian.PutUint16(sn[:], serialNo)
	buf = append(buf, sn[:]...)

	crc := crc16(buf[3:])
	crcB := [2]byte{}
	binary.BigEndian.PutUint16(crcB[:], crc)
	buf = append(buf, crcB[:]...)
	buf = append(buf, protocol.EndByte1, protocol.EndByte2)
	return buf
}

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

// loginContent returns 8 BCD bytes representing IMEI "123456789012345".
func loginContent() []byte {
	return []byte{0x12, 0x34, 0x56, 0x78, 0x90, 0x12, 0x34, 0x5F}
}

// TestServerReceivesLogin starts a real TCP server, connects a mock device,
// sends a login packet and verifies the handler is called.
func TestServerReceivesLogin(t *testing.T) {
	var (
		mu        sync.Mutex
		gotPkts   []*protocol.Packet
		gotDevice []string
	)

	handler := func(deviceID string, pkt *protocol.Packet) {
		mu.Lock()
		gotPkts = append(gotPkts, pkt)
		gotDevice = append(gotDevice, deviceID)
		mu.Unlock()
	}

	srv := server.New("127.0.0.1:0", handler)
	// Use a random free port by passing ":0"; we need to get the actual port
	// from the listener.  To do that we start the server and briefly peek at
	// the listener address via a side channel.

	// Instead: start the server on a known port range.
	// Bind to an ephemeral port first to find a free one.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // release so the server can bind

	srv = server.New(addr, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server.Start: %v", err)
	}
	defer srv.Stop()

	// Connect a simulated device.
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send login packet.
	login := buildRawPacket(protocol.ProtocolLogin, loginContent(), 1)
	if _, err := conn.Write(login); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read the server's ACK (10 bytes).
	ack := make([]byte, 10)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Read(ack); err != nil {
		t.Fatalf("read ack: %v", err)
	}

	// Give the handler goroutine time to execute.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(gotPkts) == 0 {
		t.Fatal("handler was never called")
	}
	if gotPkts[0].Type != protocol.PacketTypeLogin {
		t.Errorf("packet type: got %v, want PacketTypeLogin", gotPkts[0].Type)
	}
}

// TestServerAcknowledgesPacket verifies that the server sends a valid ACK.
func TestServerAcknowledgesPacket(t *testing.T) {
	srv := server.New("", nil)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	srv = server.New(addr, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("server.Start: %v", err)
	}
	defer srv.Stop()

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	heartbeat := buildRawPacket(protocol.ProtocolHeartbeat, []byte{}, 7)
	if _, err := conn.Write(heartbeat); err != nil {
		t.Fatalf("write: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	ack := make([]byte, 10)
	n, err := conn.Read(ack)
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	if n != 10 {
		t.Errorf("ack length: got %d, want 10", n)
	}
	if ack[0] != protocol.StartByte1 || ack[1] != protocol.StartByte1 {
		t.Error("ACK missing start bytes")
	}
	if ack[n-2] != protocol.EndByte1 || ack[n-1] != protocol.EndByte2 {
		t.Error("ACK missing end bytes")
	}
}

// TestServerStop verifies that Stop returns without deadlock.
func TestServerStop(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	srv := server.New(addr, nil)
	if err := srv.Start(); err != nil {
		t.Fatalf("server.Start: %v", err)
	}

	done := make(chan struct{})
	go func() {
		srv.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() did not return within 3 seconds")
	}
}
