// Package server implements the TCP listener that accepts connections from
// iStartek VT200 GPS trackers, decodes their binary frames, acknowledges each
// packet, and dispatches decoded data to a configurable handler.
package server

import (
	"errors"
	"io"
	"log"
	"net"
	"sync"

	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
)

// PacketHandler is called for every successfully decoded packet.
// deviceID is the IMEI of the device (set after the login packet is received;
// it is empty for the login packet itself).
type PacketHandler func(deviceID string, pkt *protocol.Packet)

// Server is a TCP server that handles VT200 device connections.
type Server struct {
	addr    string
	handler PacketHandler

	listener net.Listener
	wg       sync.WaitGroup
	quit     chan struct{}
}

// New creates a new Server that will listen on addr and call handler for each
// decoded packet.
func New(addr string, handler PacketHandler) *Server {
	return &Server{
		addr:    addr,
		handler: handler,
		quit:    make(chan struct{}),
	}
}

// Start begins listening for incoming device connections.  It returns an error
// if the listener cannot be created; otherwise it blocks until Stop is called.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.listener = ln
	log.Printf("[tcp] listening on %s", s.addr)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.acceptLoop(ln)
	}()

	return nil
}

// Stop gracefully shuts down the server, closing the listener and waiting for
// all active device sessions to finish.
func (s *Server) Stop() {
	close(s.quit)
	if s.listener != nil {
		s.listener.Close()
	}
	s.wg.Wait()
}

// acceptLoop accepts new TCP connections until the listener is closed.
func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				return
			default:
				log.Printf("[tcp] accept error: %v", err)
				continue
			}
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

// handleConn runs the packet read-loop for a single device connection.
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()
	remote := conn.RemoteAddr().String()
	log.Printf("[tcp] new connection from %s", remote)

	var deviceID string // populated after the first login packet

	for {
		pkt, err := protocol.ReadPacket(conn)
		if err != nil {
			if errors.Is(err, io.EOF) || isClosedErr(err) {
				log.Printf("[tcp] %s disconnected", remote)
			} else {
				log.Printf("[tcp] %s read error: %v", remote, err)
			}
			return
		}

		// Keep track of the device IMEI once we see a login packet.
		if pkt.Type == protocol.PacketTypeLogin && pkt.Login != nil {
			deviceID = pkt.Login.IMEI
			log.Printf("[tcp] device %s logged in from %s", deviceID, remote)
		}

		// Acknowledge every packet.
		resp := protocol.BuildResponse(pkt.Protocol, pkt.SerialNo)
		if _, err := conn.Write(resp); err != nil {
			log.Printf("[tcp] %s write error: %v", remote, err)
			return
		}

		// Dispatch to the application handler.
		if s.handler != nil {
			s.handler(deviceID, pkt)
		}
	}
}

// isClosedErr reports whether the error is a "use of closed network connection"
// sentinel.
func isClosedErr(err error) bool {
	if err == nil {
		return false
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return netErr.Err.Error() == "use of closed network connection"
	}
	return false
}
