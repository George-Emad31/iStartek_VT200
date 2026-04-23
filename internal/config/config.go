// Package config provides configuration for the iStartek VT200 integration server.
package config

import (
	"os"
	"strconv"
)

// Config holds all runtime configuration values.
type Config struct {
	// TCP server settings
	TCPAddress string // e.g. ":5000"

	// MQTT broker settings
	MQTTBroker   string // e.g. "tcp://localhost:1883"
	MQTTClientID string
	MQTTUsername string
	MQTTPassword string
	MQTTTopicPrefix string // e.g. "gps/tracker"

	// MQTT QoS level (0, 1, or 2)
	MQTTQoS byte
}

// Load returns a Config populated from environment variables.
// Each variable has a sensible default so the server can start with no configuration.
func Load() *Config {
	return &Config{
		TCPAddress:      envStr("TCP_ADDRESS", ":5000"),
		MQTTBroker:      envStr("MQTT_BROKER", "tcp://localhost:1883"),
		MQTTClientID:    envStr("MQTT_CLIENT_ID", "istartek-vt200-server"),
		MQTTUsername:    envStr("MQTT_USERNAME", ""),
		MQTTPassword:    envStr("MQTT_PASSWORD", ""),
		MQTTTopicPrefix: envStr("MQTT_TOPIC_PREFIX", "gps/tracker"),
		MQTTQoS:         envUint8("MQTT_QOS", 1),
	}
}

func envStr(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func envUint8(key string, defaultValue uint8) uint8 {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil && i >= 0 && i <= 255 {
			return uint8(i)
		}
	}
	return defaultValue
}
