# MQTT Device Tracker

A self-contained GPS device tracker with an embedded MQTT broker and real-time web dashboard.

Devices publish telemetry to the broker over MQTT. The dashboard receives updates instantly via WebSocket and displays each device on a map with GPS coordinates, signal quality, battery level, and online status.

## How it works

```
Device  →  MQTT (TCP :1883)  →  Broker  →  SQLite  →  WebSocket  →  Browser
```

- **Embedded MQTT broker** ([mochi-mqtt](https://github.com/mochi-mqtt/server)) listens on `:1883`. Devices authenticate with a shared username/password and publish JSON payloads to `devices/{id}/status`.
- **Subscriber** persists each message to SQLite and broadcasts the updated device list to all connected browsers over WebSocket.
- **Web dashboard** connects via WebSocket and renders a card per device showing a live map tile (CartoCDN), coordinates, speed, altitude, satellite count, RSSI/SNR, battery, and last-seen time. Stale devices (unseen for 48 hours) are automatically removed.

## Device payload

Devices publish JSON to `devices/{id}/status`:

```json
{
  "lat": 46.0569,
  "lon": 14.5058,
  "alt": 12.0,
  "speed": 0.0,
  "course": 0.0,
  "sats": 8,
  "hdop": 1.2,
  "battery_mv": 3900,
  "rssi": -85.0,
  "snr": 7.0
}
```

## Running

**Requirements:** `MQTT_PASSWORD` environment variable must be set.

```bash
export MQTT_PASSWORD=secret
./mqtt
# HTTP dashboard: http://localhost:8910
# MQTT broker:    localhost:1883  (user: devices)
```

Flags:

| Flag         | Default          | Description             |
| ------------ | ---------------- | ----------------------- |
| `-addr`      | `localhost:8910` | HTTP server address     |
| `-mqtt-addr` | `:1883`          | MQTT broker address     |
| `-db`        | `:memory:`       | SQLite database path    |
| `-json`      | `false`          | JSON structured logging |

## Docker

```bash
docker build -t mqtt-tracker .
docker run -e MQTT_PASSWORD=secret -p 8910:8910 -p 1883:1883 mqtt-tracker
```

## Simulator

A built-in simulator publishes fake GPS devices around Ljubljana to test the dashboard without real hardware:

```bash
./mqtt simulate --password secret --count 5 --interval 5s
```

## Development

Install tools with [mise](https://mise.jdx.dev/):

```bash
mise install
```

| Task                     | Command          |
| ------------------------ | ---------------- |
| Run (with live reload)   | `mise run watch` |
| Build binary             | `mise run build` |
| Test                     | `mise run test`  |
| Lint                     | `mise run lint`  |
| CI (build + test + lint) | `mise run ci`    |
