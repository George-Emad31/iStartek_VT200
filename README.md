# iStartek VT200 – Go Integration Server

A production-ready Go server that receives binary TCP packets from **iStartek VT200** GPS trackers (Communication Protocol v2.3), parses them, and publishes structured JSON payloads to an **MQTT** broker.

---

## Architecture

```
┌──────────────┐   TCP (binary)   ┌──────────────────────────────────────┐
│ iStartek VT200│ ────────────────► │           Go Integration Server       │
│  GPS Tracker  │                  │                                      │
└──────────────┘                  │  ┌────────┐  ┌──────────┐  ┌──────┐  │
                                  │  │TCP Srv │─►│ Protocol │─►│ MQTT │  │
                                  │  │        │  │  Parser  │  │ Pub  │  │
                                  │  └────────┘  └──────────┘  └──┬───┘  │
                                  └─────────────────────────────── │ ─────┘
                                                                   ▼
                                                          MQTT Broker
                                                    gps/tracker/<IMEI>/gps
                                                    gps/tracker/<IMEI>/alarm
                                                    gps/tracker/<IMEI>/login
```

### Packages

| Package | Description |
|---|---|
| `internal/protocol` | Binary frame parser for iStartek Communication Protocol v2.3 (Login 0x01, Heartbeat 0x08, GPS+LBS+Status 0x22, Alarm 0x26) |
| `internal/tcp` | Concurrent TCP server – accepts tracker connections, buffers frames, dispatches to handlers |
| `internal/mqtt` | MQTT publisher that serialises parsed packets to JSON and sends them to a broker |
| `internal/config` | Configuration via environment variables |
| `cmd/server` | Entry point that wires everything together |

---

## Supported Packet Types

| Protocol | Code | Description |
|---|---|---|
| Login | `0x01` | Device registration; server sends acknowledgement |
| Heartbeat | `0x08` | Keep-alive; server sends acknowledgement |
| GPS + LBS + Status | `0x22` | Location, speed, heading, altitude, I/O status |
| Alarm | `0x26` | SOS, power cut, vibration, over-speed, geo-fence, etc. |

---

## MQTT Topics & Payloads

### GPS data (`<prefix>/<IMEI>/gps`)
```json
{
  "device_id": "123456789012345",
  "timestamp": "2024-06-01T08:00:00Z",
  "latitude": 30.123456,
  "longitude": 31.678900,
  "speed_kmh": 60,
  "heading_deg": 90,
  "altitude_m": 50,
  "satellites": 8,
  "rssi": 20,
  "ignition": true,
  "odometer_m": 1000,
  "analog0_mv": 12000,
  "analog1_mv": 4100
}
```

### Alarm (`<prefix>/<IMEI>/alarm`)
```json
{
  "device_id": "123456789012345",
  "timestamp": "2024-01-10T08:30:00Z",
  "alarm": "SOS",
  "latitude": 25.0,
  "longitude": 45.0,
  "speed_kmh": 0
}
```

### Login (`<prefix>/<IMEI>/login`)
```json
{
  "device_id": "123456789012345",
  "timestamp": "2024-03-15T10:30:00Z",
  "signal": 20,
  "mcc": 460,
  "mnc": 1
}
```

---

## Configuration (Environment Variables)

| Variable | Default | Description |
|---|---|---|
| `TCP_ADDRESS` | `:5000` | Address the TCP server listens on |
| `MQTT_BROKER` | `tcp://localhost:1883` | MQTT broker URL |
| `MQTT_CLIENT_ID` | `istartek-vt200-server` | MQTT client identifier |
| `MQTT_USERNAME` | *(empty)* | MQTT username |
| `MQTT_PASSWORD` | *(empty)* | MQTT password |
| `MQTT_TOPIC_PREFIX` | `gps/tracker` | Topic prefix (`<prefix>/<IMEI>/<type>`) |
| `MQTT_QOS` | `1` | MQTT QoS level (0, 1, or 2) |

---

## Getting Started

### Prerequisites
- Go 1.21+
- An MQTT broker (e.g. [Mosquitto](https://mosquitto.org/))

### Run

```bash
# Clone the repository
git clone https://github.com/George-Emad31/iStartek_VT200.git
cd iStartek_VT200

# Run with defaults (broker on localhost:1883, TCP on :5000)
go run ./cmd/server

# Or with custom config
TCP_ADDRESS=:5000 \
MQTT_BROKER=tcp://broker.example.com:1883 \
MQTT_USERNAME=myuser \
MQTT_PASSWORD=mypassword \
go run ./cmd/server
```

### Test

```bash
go test ./...
```

---

## Protocol Reference

Frame layout (big-endian):

```
┌────────┬────────┬─────────────────┬──────────┬─────────────────────┬──────────┬──────────┬────────┐
│ Start  │ Length │   Device ID     │ Protocol │    Information      │ Serial # │ Checksum │  End   │
│ 2 bytes│ 2 bytes│   15 bytes      │  1 byte  │    N bytes          │  2 bytes │  2 bytes │ 2 bytes│
│  0x2424│        │ (IMEI, ASCII)   │          │                     │          │ (XOR)    │ 0x0D0A │
└────────┴────────┴─────────────────┴──────────┴─────────────────────┴──────────┴──────────┴────────┘
```

The **Checksum** is the XOR of all bytes from the Length field through the Serial Number field (inclusive), replicated in both bytes of the 2-byte checksum field.
