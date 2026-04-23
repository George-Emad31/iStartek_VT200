// Package mqtt provides an MQTT publisher for iStartek GPS tracker data.
package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
)

// Publisher sends tracker events to an MQTT broker.
type Publisher struct {
	client      paho.Client
	topicPrefix string
	qos         byte
}

// GPSMessage is the JSON payload published for GPS data packets.
type GPSMessage struct {
	DeviceID   string    `json:"device_id"`
	Timestamp  time.Time `json:"timestamp"`
	Latitude   float64   `json:"latitude"`
	Longitude  float64   `json:"longitude"`
	Speed      uint8     `json:"speed_kmh"`
	Heading    uint16    `json:"heading_deg"`
	Altitude   int16     `json:"altitude_m"`
	Satellites uint8     `json:"satellites"`
	RSSI       uint8     `json:"rssi"`
	Ignition   bool      `json:"ignition"`
	Odometer   uint32    `json:"odometer_m"`
	Analog0    uint16    `json:"analog0_mv"`
	Analog1    uint16    `json:"analog1_mv"`
}

// AlarmMessage is the JSON payload published for alarm packets.
type AlarmMessage struct {
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`
	Alarm     string    `json:"alarm"`
	Latitude  float64   `json:"latitude"`
	Longitude float64   `json:"longitude"`
	Speed     uint8     `json:"speed_kmh"`
}

// LoginMessage is the JSON payload published for login packets.
type LoginMessage struct {
	DeviceID  string    `json:"device_id"`
	Timestamp time.Time `json:"timestamp"`
	Signal    uint8     `json:"signal"`
	MCC       uint16    `json:"mcc"`
	MNC       uint8     `json:"mnc"`
}

// Options configures the MQTT publisher.
type Options struct {
	Broker      string
	ClientID    string
	Username    string
	Password    string
	TopicPrefix string
	QoS         byte
	// ConnectTimeout is the maximum time to wait for the broker connection.
	ConnectTimeout time.Duration
}

// NewPublisher creates and connects an MQTT Publisher.
func NewPublisher(opts Options) (*Publisher, error) {
	if opts.ConnectTimeout == 0 {
		opts.ConnectTimeout = 10 * time.Second
	}

	pahoOpts := paho.NewClientOptions().
		AddBroker(opts.Broker).
		SetClientID(opts.ClientID).
		SetUsername(opts.Username).
		SetPassword(opts.Password).
		SetConnectTimeout(opts.ConnectTimeout).
		SetAutoReconnect(true).
		SetOnConnectHandler(func(_ paho.Client) {
			log.Printf("mqtt: connected to %s", opts.Broker)
		}).
		SetConnectionLostHandler(func(_ paho.Client, err error) {
			log.Printf("mqtt: connection lost: %v", err)
		})

	client := paho.NewClient(pahoOpts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		return nil, fmt.Errorf("mqtt: connect to %s: %w", opts.Broker, token.Error())
	}

	return &Publisher{
		client:      client,
		topicPrefix: opts.TopicPrefix,
		qos:         opts.QoS,
	}, nil
}

// NewPublisherWithClient creates a Publisher using an already-connected paho client.
// This is primarily used in unit tests.
func NewPublisherWithClient(client paho.Client, topicPrefix string, qos byte) *Publisher {
	return &Publisher{
		client:      client,
		topicPrefix: topicPrefix,
		qos:         qos,
	}
}

// PublishGPS publishes a GPSPacket payload to  <topicPrefix>/<deviceID>/gps.
func (p *Publisher) PublishGPS(pkt *protocol.GPSPacket) error {
	msg := GPSMessage{
		DeviceID:   pkt.DeviceID,
		Timestamp:  pkt.Time,
		Latitude:   pkt.Latitude,
		Longitude:  pkt.Longitude,
		Speed:      pkt.Speed,
		Heading:    pkt.Heading,
		Altitude:   pkt.Altitude,
		Satellites: pkt.Satellites,
		RSSI:       pkt.RSSI,
		Ignition:   pkt.Status.Ignition,
		Odometer:   pkt.Odometer,
		Analog0:    pkt.Analog0,
		Analog1:    pkt.Analog1,
	}
	topic := fmt.Sprintf("%s/%s/gps", p.topicPrefix, pkt.DeviceID)
	return p.publish(topic, msg)
}

// PublishAlarm publishes an AlarmPacket payload to <topicPrefix>/<deviceID>/alarm.
func (p *Publisher) PublishAlarm(pkt *protocol.AlarmPacket) error {
	msg := AlarmMessage{
		DeviceID:  pkt.DeviceID,
		Timestamp: pkt.Time,
		Alarm:     alarmName(pkt.Alarm),
		Latitude:  pkt.Latitude,
		Longitude: pkt.Longitude,
		Speed:     pkt.Speed,
	}
	topic := fmt.Sprintf("%s/%s/alarm", p.topicPrefix, pkt.DeviceID)
	return p.publish(topic, msg)
}

// PublishLogin publishes a LoginPacket payload to <topicPrefix>/<deviceID>/login.
func (p *Publisher) PublishLogin(pkt *protocol.LoginPacket) error {
	msg := LoginMessage{
		DeviceID:  pkt.DeviceID,
		Timestamp: pkt.Time,
		Signal:    pkt.Signal,
		MCC:       pkt.MCC,
		MNC:       pkt.MNC,
	}
	topic := fmt.Sprintf("%s/%s/login", p.topicPrefix, pkt.DeviceID)
	return p.publish(topic, msg)
}

// Disconnect cleanly disconnects from the broker.
func (p *Publisher) Disconnect(quiesce uint) {
	p.client.Disconnect(quiesce)
}

// publish serialises payload to JSON and sends it to topic.
func (p *Publisher) publish(topic string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("mqtt: marshal payload: %w", err)
	}
	token := p.client.Publish(topic, p.qos, false, data)
	token.Wait()
	if token.Error() != nil {
		return fmt.Errorf("mqtt: publish to %s: %w", topic, token.Error())
	}
	return nil
}

// alarmName maps an AlarmType to a human-readable string.
func alarmName(a protocol.AlarmType) string {
	names := map[protocol.AlarmType]string{
		protocol.AlarmSOS:             "SOS",
		protocol.AlarmPowerCut:        "power_cut",
		protocol.AlarmVibration:       "vibration",
		protocol.AlarmEnterGeoFence:   "enter_geo_fence",
		protocol.AlarmExitGeoFence:    "exit_geo_fence",
		protocol.AlarmOverSpeed:       "over_speed",
		protocol.AlarmMovement:        "movement",
		protocol.AlarmLowBattery:      "low_battery",
		protocol.AlarmExternalPowerOff: "external_power_off",
	}
	if name, ok := names[a]; ok {
		return name
	}
	return fmt.Sprintf("unknown_0x%02X", byte(a))
}
