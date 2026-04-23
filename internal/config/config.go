// Package config provides configuration loading for the iStartek VT200 server.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all runtime configuration for the server.
type Config struct {
	// TCP listener settings
	TCPAddress string // e.g. ":8080"

	// MQTT broker settings
	MQTTBroker   string // e.g. "tcp://localhost:1883"
	MQTTClientID string
	MQTTUsername string
	MQTTPassword string
	MQTTTopicPrefix string // e.g. "tracker"

	// Logging
	LogLevel string // "debug", "info", "warn", "error"
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		TCPAddress:      getEnv("TCP_ADDRESS", ":8080"),
		MQTTBroker:      getEnv("MQTT_BROKER", "tcp://localhost:1883"),
		MQTTClientID:    getEnv("MQTT_CLIENT_ID", "istartek_vt200_server"),
		MQTTUsername:    getEnv("MQTT_USERNAME", ""),
		MQTTPassword:    getEnv("MQTT_PASSWORD", ""),
		MQTTTopicPrefix: getEnv("MQTT_TOPIC_PREFIX", "tracker"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
	}

	if cfg.TCPAddress == "" {
		return nil, fmt.Errorf("TCP_ADDRESS must not be empty")
	}
	if cfg.MQTTBroker == "" {
		return nil, fmt.Errorf("MQTT_BROKER must not be empty")
	}
	return cfg, nil
}

// getEnv returns the value of the environment variable named by key,
// or defaultValue if the variable is not set.
func getEnv(key, defaultValue string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return defaultValue
}

// getEnvInt returns the integer value of an environment variable or the
// provided default when the variable is absent or cannot be parsed.
func getEnvInt(key string, defaultValue int) int {
	raw := getEnv(key, "")
	if raw == "" {
		return defaultValue
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return defaultValue
	}
	return v
}
