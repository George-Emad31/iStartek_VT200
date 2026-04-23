// Package mqtt wraps the Eclipse Paho MQTT client and provides a simple
// Publisher for sending iStartek VT200 events to an MQTT broker.
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
)

// pahoClient is a minimal interface over paho.Client that the Publisher uses.
// Using a local interface allows tests to inject a simple mock without
// implementing the full paho.Client interface (which has many methods that are
// not relevant here).
type pahoClient interface {
	Publish(topic string, qos byte, retained bool, payload interface{}) paho.Token
	Disconnect(quiesce uint)
}

// QoS levels used when publishing messages.
const (
	QoSAtMostOnce  byte = 0
	QoSAtLeastOnce byte = 1
	QoSExactlyOnce byte = 2
)

// LocationMessage is the JSON payload published for GPS / alarm packets.
type LocationMessage struct {
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Speed     uint8     `json:"speed_kmh"`
	Course    uint16    `json:"course_deg"`
	Satellites uint8   `json:"satellites"`
	GPSValid  bool      `json:"gps_valid"`
	AlarmType *uint8    `json:"alarm_type,omitempty"`
}

// Publisher sends tracker events to an MQTT broker.
type Publisher struct {
	client      pahoClient
	topicPrefix string
}

// Options configures the MQTT connection.
type Options struct {
	Broker      string // e.g. "tcp://localhost:1883"
	ClientID    string
	Username    string
	Password    string
	TopicPrefix string // e.g. "tracker"
}

// New creates a Publisher and establishes a connection to the broker.
func New(opts Options) (*Publisher, error) {
	pahoOpts := paho.NewClientOptions().
		AddBroker(opts.Broker).
		SetClientID(opts.ClientID).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second)

	if opts.Username != "" {
		pahoOpts.SetUsername(opts.Username)
		pahoOpts.SetPassword(opts.Password)
	}

	pahoOpts.OnConnectionLost = func(_ paho.Client, err error) {
		log.Printf("[mqtt] connection lost: %v", err)
	}
	pahoOpts.OnConnect = func(_ paho.Client) {
		log.Printf("[mqtt] connected to %s", opts.Broker)
	}

	client := paho.NewClient(pahoOpts)

	token := client.Connect()
	if token.WaitTimeout(10*time.Second) && token.Error() != nil {
		return nil, fmt.Errorf("mqtt connect: %w", token.Error())
	}

	return &Publisher{
		client:      client,
		topicPrefix: opts.TopicPrefix,
	}, nil
}

// NewWithClient creates a Publisher using an already-connected client.
// Intended for unit testing with a mock client.
func NewWithClient(client pahoClient, topicPrefix string) *Publisher {
	return &Publisher{client: client, topicPrefix: topicPrefix}
}

// PublishPacket inspects the decoded packet and publishes an appropriate
// message to the MQTT broker.  Login and heartbeat packets produce no message.
func (p *Publisher) PublishPacket(deviceID string, pkt *protocol.Packet) error {
	switch pkt.Type {
	case protocol.PacketTypeGPS:
		return p.publishLocation(deviceID, pkt.GPS, false)
	case protocol.PacketTypeGPSAlarm:
		return p.publishLocation(deviceID, pkt.GPS, true)
	case protocol.PacketTypeLogin, protocol.PacketTypeHeartbeat, protocol.PacketTypeUnknown:
		// Nothing to publish.
		return nil
	default:
		return nil
	}
}

// publishLocation serialises a GPSData struct to JSON and publishes it.
func (p *Publisher) publishLocation(deviceID string, gps *protocol.GPSData, isAlarm bool) error {
	msg := LocationMessage{
		DeviceID:   deviceID,
		Timestamp:  gps.Timestamp,
		Latitude:   gps.Latitude,
		Longitude:  gps.Longitude,
		Speed:      gps.Speed,
		Course:     gps.Course,
		Satellites: gps.Satellites,
		GPSValid:   gps.GPSValid,
	}
	if isAlarm {
		at := gps.AlarmType
		msg.AlarmType = &at
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal location: %w", err)
	}

	topic := p.locationTopic(deviceID, isAlarm)
	token := p.client.Publish(topic, QoSAtLeastOnce, false, payload)
	token.Wait()
	if err := token.Error(); err != nil {
		return fmt.Errorf("mqtt publish to %s: %w", topic, err)
	}
	return nil
}

// locationTopic returns the MQTT topic for a device's location (or alarm).
func (p *Publisher) locationTopic(deviceID string, isAlarm bool) string {
	if isAlarm {
		return fmt.Sprintf("%s/%s/alarm", p.topicPrefix, deviceID)
	}
	return fmt.Sprintf("%s/%s/location", p.topicPrefix, deviceID)
}

// Disconnect cleanly shuts down the MQTT connection.
func (p *Publisher) Disconnect() {
	p.client.Disconnect(250)
}
