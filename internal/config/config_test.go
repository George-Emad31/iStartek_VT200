package config_test

import (
	"os"
	"testing"

	"github.com/George-Emad31/iStartek_VT200/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	// Ensure no interfering env vars.
	os.Unsetenv("TCP_ADDRESS")
	os.Unsetenv("MQTT_BROKER")
	os.Unsetenv("MQTT_CLIENT_ID")
	os.Unsetenv("MQTT_TOPIC_PREFIX")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.TCPAddress != ":8080" {
		t.Errorf("TCPAddress: got %q, want %q", cfg.TCPAddress, ":8080")
	}
	if cfg.MQTTBroker != "tcp://localhost:1883" {
		t.Errorf("MQTTBroker: got %q, want %q", cfg.MQTTBroker, "tcp://localhost:1883")
	}
	if cfg.MQTTClientID != "istartek_vt200_server" {
		t.Errorf("MQTTClientID: got %q, want %q", cfg.MQTTClientID, "istartek_vt200_server")
	}
	if cfg.MQTTTopicPrefix != "tracker" {
		t.Errorf("MQTTTopicPrefix: got %q, want %q", cfg.MQTTTopicPrefix, "tracker")
	}
}

func TestLoadFromEnv(t *testing.T) {
	os.Setenv("TCP_ADDRESS", ":9090")
	os.Setenv("MQTT_BROKER", "tcp://broker.example.com:1883")
	os.Setenv("MQTT_CLIENT_ID", "myClient")
	os.Setenv("MQTT_TOPIC_PREFIX", "vehicles")
	defer func() {
		os.Unsetenv("TCP_ADDRESS")
		os.Unsetenv("MQTT_BROKER")
		os.Unsetenv("MQTT_CLIENT_ID")
		os.Unsetenv("MQTT_TOPIC_PREFIX")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.TCPAddress != ":9090" {
		t.Errorf("TCPAddress: got %q, want %q", cfg.TCPAddress, ":9090")
	}
	if cfg.MQTTBroker != "tcp://broker.example.com:1883" {
		t.Errorf("MQTTBroker: got %q, want %q", cfg.MQTTBroker, "tcp://broker.example.com:1883")
	}
	if cfg.MQTTClientID != "myClient" {
		t.Errorf("MQTTClientID: got %q, want %q", cfg.MQTTClientID, "myClient")
	}
	if cfg.MQTTTopicPrefix != "vehicles" {
		t.Errorf("MQTTTopicPrefix: got %q, want %q", cfg.MQTTTopicPrefix, "vehicles")
	}
}
