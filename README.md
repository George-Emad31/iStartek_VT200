# iStartek VT200 — TCP → MQTT Gateway (Go)

A production-ready Go service that accepts binary TCP connections from
**iStartek VT200** GPS trackers, decodes their proprietary frames, and
publishes location / alarm events to an **MQTT** broker.

---

## Architecture

```
VT200 Device ──TCP──► TCP Server ──► Protocol Parser ──► MQTT Publisher ──► Broker
                          │                                        │
                          └── sends ACK back to device             └── topic: tracker/{IMEI}/location
                                                                        topic: tracker/{IMEI}/alarm
```

### Packet types handled

| Protocol byte | Name       | Action                                         |
|---------------|------------|------------------------------------------------|
| `0x01`        | Login      | Extract IMEI, send ACK, no MQTT publish        |
| `0x10`        | GPS        | Parse location, publish to `.../location`      |
| `0x12`        | GPS + LBS  | Parse location, publish to `.../location`      |
| `0x13`        | Status     | Heartbeat — send ACK only                      |
| `0x16`        | Alarm      | Parse location + alarm, publish to `.../alarm` |
| `0x22`        | GPS ext    | Parse location, publish to `.../location`      |
| `0x23`        | Heartbeat  | Send ACK only                                  |

---

## Project structure

```
.
├── cmd/
│   └── server/
│       └── main.go           # Entry point
├── internal/
│   ├── config/
│   │   ├── config.go         # Env-var configuration
│   │   └── config_test.go
│   ├── protocol/
│   │   ├── packet.go         # Type definitions
│   │   ├── parser.go         # Binary parser + CRC + response builder
│   │   └── parser_test.go
│   ├── server/
│   │   ├── tcp_server.go     # TCP listener + connection handler
│   │   └── tcp_server_test.go
│   └── mqtt/
│       ├── publisher.go      # MQTT publisher (JSON payloads)
│       └── publisher_test.go
├── go.mod
└── go.sum
```

---

## Configuration

All settings are loaded from **environment variables**:

| Variable            | Default                 | Description                        |
|---------------------|-------------------------|------------------------------------|
| `TCP_ADDRESS`       | `:8080`                 | TCP address to listen on           |
| `MQTT_BROKER`       | `tcp://localhost:1883`  | MQTT broker URL                    |
| `MQTT_CLIENT_ID`    | `istartek_vt200_server` | MQTT client identifier             |
| `MQTT_USERNAME`     | *(empty)*               | MQTT username (optional)           |
| `MQTT_PASSWORD`     | *(empty)*               | MQTT password (optional)           |
| `MQTT_TOPIC_PREFIX` | `tracker`               | Topic prefix (`{prefix}/{IMEI}/…`) |
| `LOG_LEVEL`         | `info`                  | Logging verbosity                  |

---

## Build & Run

```bash
# Build
go build -o gateway ./cmd/server

# Run with defaults (needs local MQTT broker on port 1883)
./gateway

# Run with custom settings
TCP_ADDRESS=:9090 \
MQTT_BROKER=tcp://mqtt.example.com:1883 \
MQTT_TOPIC_PREFIX=vehicles \
./gateway
```

---

## Testing

```bash
go test ./... -v
```

All packages include unit tests.  The server tests use real loopback TCP
connections with ephemeral ports.  The MQTT tests use an in-process mock client
so no broker is required.

---

## MQTT payload format

### Location (`tracker/{IMEI}/location`)

```json
{
  "device_id":  "123456789012345",
  "timestamp":  "2024-06-15T10:30:00Z",
  "latitude":   30.0,
  "longitude":  31.5,
  "speed_kmh":  60,
  "course_deg": 90,
  "satellites": 8,
  "gps_valid":  true
}
```

### Alarm (`tracker/{IMEI}/alarm`)

Same as above, plus:

```json
{
  "alarm_type": 9
}
```

Common alarm type values:

| Value  | Meaning         |
|--------|-----------------|
| `0x01` | SOS             |
| `0x02` | Power cut       |
| `0x04` | Vibration       |
| `0x09` | Overspeed       |
| `0x0E` | Geo-fence enter |
| `0x0F` | Geo-fence exit  |
