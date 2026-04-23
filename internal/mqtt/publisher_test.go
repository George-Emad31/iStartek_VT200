package mqtt_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mqttpkg "github.com/George-Emad31/iStartek_VT200/internal/mqtt"
	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
)

// ─── Mock MQTT client ─────────────────────────────────────────────────────────

type publishedMsg struct {
	topic   string
	payload []byte
	qos     byte
}

type mockToken struct{}

func (m *mockToken) Wait() bool          { return true }
func (m *mockToken) WaitTimeout(_ time.Duration) bool { return true }
func (m *mockToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (m *mockToken) Error() error { return nil }

type mockClient struct {
	mu       sync.Mutex
	messages []publishedMsg
}

func (c *mockClient) IsConnected() bool                               { return true }
func (c *mockClient) IsConnectionOpen() bool                          { return true }
func (c *mockClient) Connect() paho.Token                             { return &mockToken{} }
func (c *mockClient) Disconnect(_ uint)                               {}
func (c *mockClient) Subscribe(_ string, _ byte, _ paho.MessageHandler) paho.Token {
	return &mockToken{}
}
func (c *mockClient) SubscribeMultiple(_ map[string]byte, _ paho.MessageHandler) paho.Token {
	return &mockToken{}
}
func (c *mockClient) Unsubscribe(_ ...string) paho.Token { return &mockToken{} }
func (c *mockClient) AddRoute(_ string, _ paho.MessageHandler) {}
func (c *mockClient) OptionsReader() paho.ClientOptionsReader {
	return paho.ClientOptionsReader{}
}

func (c *mockClient) Publish(topic string, qos byte, _ bool, payload interface{}) paho.Token {
	c.mu.Lock()
	defer c.mu.Unlock()
	var data []byte
	switch v := payload.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	}
	c.messages = append(c.messages, publishedMsg{topic: topic, payload: data, qos: qos})
	return &mockToken{}
}

func (c *mockClient) last() publishedMsg {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.messages) == 0 {
		return publishedMsg{}
	}
	return c.messages[len(c.messages)-1]
}

// ─── Tests ────────────────────────────────────────────────────────────────────

const testIMEI = "123456789012345"
const topicPrefix = "gps/tracker"

func newPublisher(c *mockClient) *mqttpkg.Publisher {
	return mqttpkg.NewPublisherWithClient(c, topicPrefix, 1)
}

func TestPublishGPS(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	ts := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	pkt := &protocol.GPSPacket{
		DeviceID:   testIMEI,
		SerialNo:   1,
		Time:       ts,
		Latitude:   30.12345,
		Longitude:  31.67890,
		Speed:      80,
		Heading:    270,
		Altitude:   100,
		Satellites: 10,
		RSSI:       25,
		Analog0:    12000,
		Analog1:    4100,
		Odometer:   5000,
		Status:     protocol.GPSStatus{Ignition: true, ACC: true, GPSTracking: true},
	}

	err := pub.PublishGPS(pkt)
	require.NoError(t, err)

	msg := mc.last()
	assert.Equal(t, "gps/tracker/"+testIMEI+"/gps", msg.topic)
	assert.Equal(t, byte(1), msg.qos)

	var payload mqttpkg.GPSMessage
	require.NoError(t, json.Unmarshal(msg.payload, &payload))
	assert.Equal(t, testIMEI, payload.DeviceID)
	assert.InDelta(t, 30.12345, payload.Latitude, 1e-5)
	assert.InDelta(t, 31.67890, payload.Longitude, 1e-5)
	assert.Equal(t, uint8(80), payload.Speed)
	assert.Equal(t, uint16(270), payload.Heading)
	assert.Equal(t, int16(100), payload.Altitude)
	assert.True(t, payload.Ignition)
	assert.Equal(t, uint32(5000), payload.Odometer)
	assert.Equal(t, ts.UTC(), payload.Timestamp.UTC())
}

func TestPublishAlarm_SOS(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	ts := time.Date(2024, 1, 10, 8, 30, 0, 0, time.UTC)
	pkt := &protocol.AlarmPacket{
		DeviceID:  testIMEI,
		SerialNo:  2,
		Time:      ts,
		Alarm:     protocol.AlarmSOS,
		Latitude:  25.0,
		Longitude: 45.0,
		Speed:     0,
	}

	err := pub.PublishAlarm(pkt)
	require.NoError(t, err)

	msg := mc.last()
	assert.Equal(t, "gps/tracker/"+testIMEI+"/alarm", msg.topic)

	var payload mqttpkg.AlarmMessage
	require.NoError(t, json.Unmarshal(msg.payload, &payload))
	assert.Equal(t, "SOS", payload.Alarm)
	assert.Equal(t, testIMEI, payload.DeviceID)
	assert.InDelta(t, 25.0, payload.Latitude, 1e-6)
}

func TestPublishAlarm_OverSpeed(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	pkt := &protocol.AlarmPacket{
		DeviceID: testIMEI,
		Alarm:    protocol.AlarmOverSpeed,
		Speed:    120,
	}

	require.NoError(t, pub.PublishAlarm(pkt))

	var payload mqttpkg.AlarmMessage
	require.NoError(t, json.Unmarshal(mc.last().payload, &payload))
	assert.Equal(t, "over_speed", payload.Alarm)
	assert.Equal(t, uint8(120), payload.Speed)
}

func TestPublishAlarm_Unknown(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	pkt := &protocol.AlarmPacket{
		DeviceID: testIMEI,
		Alarm:    protocol.AlarmType(0xFF),
	}
	require.NoError(t, pub.PublishAlarm(pkt))

	var payload mqttpkg.AlarmMessage
	require.NoError(t, json.Unmarshal(mc.last().payload, &payload))
	assert.Equal(t, "unknown_0xFF", payload.Alarm)
}

func TestPublishLogin(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	ts := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	pkt := &protocol.LoginPacket{
		DeviceID: testIMEI,
		SerialNo: 3,
		Time:     ts,
		Signal:   18,
		MCC:      460,
		MNC:      1,
	}

	require.NoError(t, pub.PublishLogin(pkt))

	msg := mc.last()
	assert.Equal(t, "gps/tracker/"+testIMEI+"/login", msg.topic)

	var payload mqttpkg.LoginMessage
	require.NoError(t, json.Unmarshal(msg.payload, &payload))
	assert.Equal(t, uint8(18), payload.Signal)
	assert.Equal(t, uint16(460), payload.MCC)
	assert.Equal(t, uint8(1), payload.MNC)
}

func TestPublishMultiple_TopicsAreSeparate(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	require.NoError(t, pub.PublishLogin(&protocol.LoginPacket{DeviceID: testIMEI}))
	require.NoError(t, pub.PublishGPS(&protocol.GPSPacket{DeviceID: testIMEI}))
	require.NoError(t, pub.PublishAlarm(&protocol.AlarmPacket{DeviceID: testIMEI, Alarm: protocol.AlarmSOS}))

	mc.mu.Lock()
	defer mc.mu.Unlock()
	require.Len(t, mc.messages, 3)
	assert.Contains(t, mc.messages[0].topic, "/login")
	assert.Contains(t, mc.messages[1].topic, "/gps")
	assert.Contains(t, mc.messages[2].topic, "/alarm")
}
