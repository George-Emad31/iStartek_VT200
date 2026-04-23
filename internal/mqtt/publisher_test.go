package mqtt_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"

	mqttpkg "github.com/George-Emad31/iStartek_VT200/internal/mqtt"
	"github.com/George-Emad31/iStartek_VT200/internal/protocol"
)

// --- Mock MQTT client ---

// mockToken satisfies paho.Token.
type mockToken struct{ err error }

func (t *mockToken) Wait() bool                       { return true }
func (t *mockToken) WaitTimeout(_ time.Duration) bool  { return true }
func (t *mockToken) Done() <-chan struct{}              { ch := make(chan struct{}); close(ch); return ch }
func (t *mockToken) Error() error                      { return t.err }

// mockClient captures published messages.  It only implements the two methods
// that Publisher actually calls (Publish + Disconnect), which matches the
// unexported pahoClient interface defined in the mqtt package.
type mockClient struct {
	mu       sync.Mutex
	messages []capturedMsg
}

type capturedMsg struct {
	topic   string
	payload []byte
}

func (m *mockClient) Publish(topic string, _ byte, _ bool, payload interface{}) paho.Token {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b []byte
	switch v := payload.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	}
	m.messages = append(m.messages, capturedMsg{topic: topic, payload: b})
	return &mockToken{}
}

func (m *mockClient) Disconnect(_ uint) {}

func (m *mockClient) lastMessage() *capturedMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return nil
	}
	msg := m.messages[len(m.messages)-1]
	return &msg
}

// --- Tests ---

func newPublisher(mc *mockClient) *mqttpkg.Publisher {
	return mqttpkg.NewWithClient(mc, "tracker")
}

func gpsPacket(lat, lon float64, alarm bool) *protocol.Packet {
	gps := &protocol.GPSData{
		Timestamp:  time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC),
		Latitude:   lat,
		Longitude:  lon,
		Speed:      80,
		Course:     270,
		Satellites: 9,
		GPSValid:   true,
	}
	pktType := protocol.PacketTypeGPS
	if alarm {
		pktType = protocol.PacketTypeGPSAlarm
		at := uint8(0x01)
		gps.AlarmType = at
	}
	return &protocol.Packet{
		Type:  pktType,
		GPS:   gps,
	}
}

func TestPublishGPS(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	pkt := gpsPacket(30.1, 31.2, false)
	if err := pub.PublishPacket("IMEI123", pkt); err != nil {
		t.Fatalf("PublishPacket error: %v", err)
	}

	msg := mc.lastMessage()
	if msg == nil {
		t.Fatal("no message published")
	}
	if msg.topic != "tracker/IMEI123/location" {
		t.Errorf("topic: got %q, want %q", msg.topic, "tracker/IMEI123/location")
	}

	var loc mqttpkg.LocationMessage
	if err := json.Unmarshal(msg.payload, &loc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loc.DeviceID != "IMEI123" {
		t.Errorf("DeviceID: got %q, want %q", loc.DeviceID, "IMEI123")
	}
	if !approxEqual(loc.Latitude, 30.1, 0.0001) {
		t.Errorf("Latitude: got %f, want 30.1", loc.Latitude)
	}
	if !approxEqual(loc.Longitude, 31.2, 0.0001) {
		t.Errorf("Longitude: got %f, want 31.2", loc.Longitude)
	}
	if loc.AlarmType != nil {
		t.Error("AlarmType should be nil for GPS packet")
	}
}

func TestPublishAlarm(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	pkt := gpsPacket(25.0, 55.0, true)
	if err := pub.PublishPacket("IMEI456", pkt); err != nil {
		t.Fatalf("PublishPacket error: %v", err)
	}

	msg := mc.lastMessage()
	if msg == nil {
		t.Fatal("no message published")
	}
	if msg.topic != "tracker/IMEI456/alarm" {
		t.Errorf("topic: got %q, want %q", msg.topic, "tracker/IMEI456/alarm")
	}

	var loc mqttpkg.LocationMessage
	if err := json.Unmarshal(msg.payload, &loc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loc.AlarmType == nil {
		t.Fatal("AlarmType should not be nil for alarm packet")
	}
	if *loc.AlarmType != 0x01 {
		t.Errorf("AlarmType: got 0x%02X, want 0x01", *loc.AlarmType)
	}
}

func TestPublishLogin_NoMessage(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	pkt := &protocol.Packet{
		Type:  protocol.PacketTypeLogin,
		Login: &protocol.LoginData{IMEI: "123456789012345"},
	}
	if err := pub.PublishPacket("123456789012345", pkt); err != nil {
		t.Fatalf("PublishPacket error: %v", err)
	}
	if mc.lastMessage() != nil {
		t.Error("expected no message for login packet")
	}
}

func TestPublishHeartbeat_NoMessage(t *testing.T) {
	mc := &mockClient{}
	pub := newPublisher(mc)

	pkt := &protocol.Packet{Type: protocol.PacketTypeHeartbeat}
	if err := pub.PublishPacket("dev1", pkt); err != nil {
		t.Fatalf("PublishPacket error: %v", err)
	}
	if mc.lastMessage() != nil {
		t.Error("expected no message for heartbeat packet")
	}
}

// --- helpers ---

func approxEqual(a, b, tol float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tol
}
