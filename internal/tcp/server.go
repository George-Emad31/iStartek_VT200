// Package tcp provides a TCP server that accepts connections from iStartek VT200
// GPS trackers, reads complete packet frames, and dispatches them to a handler.
package tcp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
)

const (
	// readTimeout is the per-read deadline applied to each TCP connection.
	readTimeout = 5 * time.Minute
	// maxFrameSize is the largest frame we are willing to buffer (safety limit).
	maxFrameSize = 4096
)

// Handler is called for every successfully parsed packet.
type Handler interface {
	// OnLogin is called when a device sends a login packet.
	OnLogin(conn net.Conn, pkt *protocol.LoginPacket)
	// OnHeartbeat is called for heartbeat / keep-alive packets.
	OnHeartbeat(conn net.Conn, pkt *protocol.HeartbeatPacket)
	// OnGPS is called when GPS data is received.
	OnGPS(conn net.Conn, pkt *protocol.GPSPacket)
	// OnAlarm is called when an alarm packet is received.
	OnAlarm(conn net.Conn, pkt *protocol.AlarmPacket)
}

// Server is a TCP server that reads iStartek VT200 packets.
type Server struct {
	address  string
	handler  Handler
	listener net.Listener
	wg       sync.WaitGroup
	once     sync.Once
	quit     chan struct{}
}

// NewServer creates a Server that will listen on address and dispatch packets to handler.
func NewServer(address string, handler Handler) *Server {
	return &Server{
		address: address,
		handler: handler,
		quit:    make(chan struct{}),
	}
}

// Start begins listening for TCP connections.
// It returns an error if the listener cannot be established.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	s.listener = ln
	log.Printf("tcp: listening on %s", ln.Addr())

	s.wg.Add(1)
	go s.acceptLoop(ln)
	return nil
}

// Stop gracefully shuts down the server, waiting for all active connections to finish.
func (s *Server) Stop() {
	s.once.Do(func() {
		close(s.quit)
		if s.listener != nil {
			_ = s.listener.Close()
		}
	})
	s.wg.Wait()
}

// Addr returns the network address the server is listening on.
// Returns nil if the server has not been started.
func (s *Server) Addr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// acceptLoop accepts incoming connections until the server is stopped.
func (s *Server) acceptLoop(ln net.Listener) {
	defer s.wg.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return // expected on shutdown
			default:
				log.Printf("tcp: accept error: %v", err)
				return
			}
		}
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

// handleConn processes packets from a single client connection.
func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer conn.Close()

	remote := conn.RemoteAddr().String()
	log.Printf("tcp: new connection from %s", remote)
	defer log.Printf("tcp: connection closed %s", remote)

	buf := make([]byte, maxFrameSize)
	var accumulated []byte

	for {
		select {
		case <-s.quit:
			return
		default:
		}

		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return
		}

		n, err := conn.Read(buf)
		if n > 0 {
			accumulated = append(accumulated, buf[:n]...)
			accumulated = s.processBuffer(conn, accumulated)
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !isTimeout(err) {
				log.Printf("tcp: read error from %s: %v", remote, err)
			}
			return
		}
	}
}

// processBuffer extracts and dispatches all complete frames from buf.
// It returns the remaining (unprocessed) bytes.
func (s *Server) processBuffer(conn net.Conn, buf []byte) []byte {
	for {
		// Find start marker $$
		start := bytes.Index(buf, []byte{0x24, 0x24})
		if start < 0 {
			return nil // no start marker found, discard everything
		}
		buf = buf[start:] // trim leading garbage

		// Need at least 4 bytes to read the Length field
		if len(buf) < 4 {
			return buf
		}

		// The Length field tells us how many bytes follow the Length field itself,
		// before the End marker. Total frame = 2(start) + 2(length) + length + 2(end)
		lengthField := int(binary.BigEndian.Uint16(buf[2:4]))
		frameSize := 2 + 2 + lengthField + 2
		if frameSize > maxFrameSize {
			log.Printf("tcp: oversized frame (%d bytes), discarding", frameSize)
			buf = buf[2:] // skip this start marker and try again
			continue
		}

		if len(buf) < frameSize {
			return buf // wait for more data
		}

		frame := buf[:frameSize]
		raw, err := protocol.ParseFrame(frame)
		if err != nil {
			log.Printf("tcp: parse frame error: %v", err)
			buf = buf[2:] // skip and continue
			continue
		}

		s.dispatch(conn, raw)
		buf = buf[frameSize:]
	}
}

// dispatch routes a parsed RawPacket to the appropriate Handler method.
func (s *Server) dispatch(conn net.Conn, raw *protocol.RawPacket) {
	switch raw.Protocol {
	case protocol.ProtocolLogin:
		pkt, err := protocol.DecodeLogin(raw)
		if err != nil {
			log.Printf("tcp: decode login error: %v", err)
			return
		}
		// Send login acknowledgement
		if _, err := conn.Write(protocol.BuildLoginResponse(raw.SerialNo)); err != nil {
			log.Printf("tcp: write login response: %v", err)
		}
		s.handler.OnLogin(conn, pkt)

	case protocol.ProtocolHeartbeat:
		pkt, err := protocol.DecodeHeartbeat(raw)
		if err != nil {
			log.Printf("tcp: decode heartbeat error: %v", err)
			return
		}
		if _, err := conn.Write(protocol.BuildHeartbeatResponse(raw.SerialNo)); err != nil {
			log.Printf("tcp: write heartbeat response: %v", err)
		}
		s.handler.OnHeartbeat(conn, pkt)

	case protocol.ProtocolGPSData:
		pkt, err := protocol.DecodeGPS(raw)
		if err != nil {
			log.Printf("tcp: decode GPS error: %v", err)
			return
		}
		if _, err := conn.Write(protocol.BuildGPSResponse(raw.SerialNo)); err != nil {
			log.Printf("tcp: write GPS response: %v", err)
		}
		s.handler.OnGPS(conn, pkt)

	case protocol.ProtocolAlarm:
		pkt, err := protocol.DecodeAlarm(raw)
		if err != nil {
			log.Printf("tcp: decode alarm error: %v", err)
			return
		}
		s.handler.OnAlarm(conn, pkt)

	default:
		log.Printf("tcp: unknown protocol 0x%02X, ignoring", raw.Protocol)
	}
}

// isTimeout returns true if err is a network timeout.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
