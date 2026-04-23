// Command server starts the iStartek VT200 GPS tracker integration server.
//
// The server:
//  1. Listens on a TCP port for incoming tracker connections.
//  2. Parses the binary iStartek Communication Protocol v2.3 frames.
//  3. Publishes structured JSON payloads to an MQTT broker.
//
// Configuration is done via environment variables (see internal/config/config.go).
package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/George-Emad31/iStartek_VT200/internal/config"
	mqttpkg "github.com/George-Emad31/iStartek_VT200/internal/mqtt"
	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
	tcppkg "github.com/George-Emad31/iStartek_VT200/internal/tcp"
)

func main() {
	cfg := config.Load()

	log.Printf("iStartek VT200 integration server starting")
	log.Printf("TCP address:   %s", cfg.TCPAddress)
	log.Printf("MQTT broker:   %s", cfg.MQTTBroker)
	log.Printf("MQTT topic:    %s/<device_id>/{gps|alarm|login}", cfg.MQTTTopicPrefix)

	// Connect to MQTT broker
	pub, err := mqttpkg.NewPublisher(mqttpkg.Options{
		Broker:      cfg.MQTTBroker,
		ClientID:    cfg.MQTTClientID,
		Username:    cfg.MQTTUsername,
		Password:    cfg.MQTTPassword,
		TopicPrefix: cfg.MQTTTopicPrefix,
		QoS:         cfg.MQTTQoS,
	})
	if err != nil {
		log.Fatalf("MQTT connect: %v", err)
	}
	defer pub.Disconnect(250)

	// Start TCP server
	handler := &trackerHandler{publisher: pub}
	srv := tcppkg.NewServer(cfg.TCPAddress, handler)
	if err := srv.Start(); err != nil {
		log.Fatalf("TCP server start: %v", err)
	}
	defer srv.Stop()

	log.Printf("Server ready, press Ctrl+C to stop")

	// Wait for shutdown signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Printf("Shutting down...")
}

// trackerHandler implements tcp.Handler and publishes events to MQTT.
type trackerHandler struct {
	publisher *mqttpkg.Publisher
}

func (h *trackerHandler) OnLogin(conn net.Conn, pkt *protocol.LoginPacket) {
	log.Printf("[%s] LOGIN  device=%s signal=%d MCC=%d MNC=%d",
		conn.RemoteAddr(), pkt.DeviceID, pkt.Signal, pkt.MCC, pkt.MNC)
	if err := h.publisher.PublishLogin(pkt); err != nil {
		log.Printf("[%s] MQTT publish login error: %v", conn.RemoteAddr(), err)
	}
}

func (h *trackerHandler) OnHeartbeat(conn net.Conn, pkt *protocol.HeartbeatPacket) {
	log.Printf("[%s] HEARTBEAT device=%s serial=%d",
		conn.RemoteAddr(), pkt.DeviceID, pkt.SerialNo)
}

func (h *trackerHandler) OnGPS(conn net.Conn, pkt *protocol.GPSPacket) {
	log.Printf("[%s] GPS device=%s lat=%.6f lon=%.6f speed=%d alt=%d sats=%d ignition=%v",
		conn.RemoteAddr(), pkt.DeviceID,
		pkt.Latitude, pkt.Longitude,
		pkt.Speed, pkt.Altitude, pkt.Satellites, pkt.Status.Ignition)
	if err := h.publisher.PublishGPS(pkt); err != nil {
		log.Printf("[%s] MQTT publish GPS error: %v", conn.RemoteAddr(), err)
	}
}

func (h *trackerHandler) OnAlarm(conn net.Conn, pkt *protocol.AlarmPacket) {
	log.Printf("[%s] ALARM device=%s type=0x%02X lat=%.6f lon=%.6f",
		conn.RemoteAddr(), pkt.DeviceID, pkt.Alarm, pkt.Latitude, pkt.Longitude)
	if err := h.publisher.PublishAlarm(pkt); err != nil {
		log.Printf("[%s] MQTT publish alarm error: %v", conn.RemoteAddr(), err)
	}
}
