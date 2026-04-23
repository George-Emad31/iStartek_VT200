// Command server is the main entry point for the iStartek VT200 TCP→MQTT
// gateway.  It reads configuration from environment variables, starts a TCP
// listener for device connections, decodes incoming VT200 binary frames, and
// publishes location / alarm events to an MQTT broker.
//
// Configuration (environment variables):
//
//	TCP_ADDRESS       TCP address to listen on (default: ":8080")
//	MQTT_BROKER       MQTT broker URL (default: "tcp://localhost:1883")
//	MQTT_CLIENT_ID    MQTT client identifier (default: "istartek_vt200_server")
//	MQTT_USERNAME     MQTT username (optional)
//	MQTT_PASSWORD     MQTT password (optional)
//	MQTT_TOPIC_PREFIX MQTT topic prefix (default: "tracker")
//	LOG_LEVEL         Logging verbosity (default: "info")
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/George-Emad31/iStartek_VT200/internal/config"
	mqttpkg "github.com/George-Emad31/iStartek_VT200/internal/mqtt"
	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
	"github.com/George-Emad31/iStartek_VT200/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	// Connect to MQTT broker.
	pub, err := mqttpkg.New(mqttpkg.Options{
		Broker:      cfg.MQTTBroker,
		ClientID:    cfg.MQTTClientID,
		Username:    cfg.MQTTUsername,
		Password:    cfg.MQTTPassword,
		TopicPrefix: cfg.MQTTTopicPrefix,
	})
	if err != nil {
		log.Fatalf("mqtt connect: %v", err)
	}
	defer pub.Disconnect()

	// Create the TCP server with a packet handler that publishes to MQTT.
	srv := server.New(cfg.TCPAddress, func(deviceID string, pkt *protocol.Packet) {
		if err := pub.PublishPacket(deviceID, pkt); err != nil {
			log.Printf("[handler] publish error for device %s: %v", deviceID, err)
		}
	})

	if err := srv.Start(); err != nil {
		log.Fatalf("server start: %v", err)
	}
	log.Printf("iStartek VT200 gateway running (TCP %s → MQTT %s)", cfg.TCPAddress, cfg.MQTTBroker)

	// Wait for SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down…")
	srv.Stop()
}
