package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jarv/mqtt/db"
)

// MeshtasticPacket is the top-level JSON envelope published by Meshtastic nodes.
type MeshtasticPacket struct {
	From      uint32          `json:"from"`
	Sender    string          `json:"sender"`
	Timestamp int64           `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

// PositionPayload is the payload for type=position packets.
type PositionPayload struct {
	LatitudeI   int64   `json:"latitude_i"`
	LongitudeI  int64   `json:"longitude_i"`
	Altitude    float64 `json:"altitude"`
	GroundSpeed float64 `json:"ground_speed"`
	SatsInView  int64   `json:"sats_in_view"`
}

// TelemetryPayload is the payload for type=telemetry packets.
type TelemetryPayload struct {
	BatteryLevel float64 `json:"battery_level"`
	Voltage      float64 `json:"voltage"`
	ChannelUtil  float64 `json:"channel_utilization"`
	AirUtilTX    float64 `json:"air_util_tx"`
}

// DeviceMessage is sent over WebSocket to browsers.
type DeviceMessage struct {
	Type string       `json:"type"`
	Data []DeviceView `json:"data"`
}

// DeviceView is the browser-facing representation of a device.
type DeviceView struct {
	ID           string    `json:"id"`
	Lat          float64   `json:"lat"`
	Lon          float64   `json:"lon"`
	Alt          float64   `json:"alt"`
	Speed        float64   `json:"speed"`
	Sats         int64     `json:"sats"`
	BatteryLevel int64     `json:"battery_level"`
	Online       bool      `json:"online"`
	LastSeen     time.Time `json:"last_seen"`
}

// nodeID returns the canonical hex node ID string for a uint32 node number.
func nodeID(from uint32) string {
	return fmt.Sprintf("!%08x", from)
}

// Subscriber handles incoming MQTT messages and persists them.
type Subscriber struct {
	queries *db.Queries
	cm      *ConnectionManager
}

func NewSubscriber(queries *db.Queries, cm *ConnectionManager) *Subscriber {
	return &Subscriber{queries: queries, cm: cm}
}

// HandleMessage is called by the broker on every published message.
func (s *Subscriber) HandleMessage(topic string, payload []byte) {
	// Only process JSON topics: msh/{region}/2/json/{channel}/{node}
	if !isMeshtasticJSONTopic(topic) {
		return
	}

	var pkt MeshtasticPacket
	if err := json.Unmarshal(payload, &pkt); err != nil {
		slog.Warn("failed to parse meshtastic packet", "topic", topic, "err", err)
		return
	}

	id := nodeID(pkt.From)

	switch pkt.Type {
	case "position":
		s.handlePosition(id, pkt.Payload)
	case "telemetry":
		s.handleTelemetry(id, pkt.Payload)
	default:
		// ignore other packet types (nodeinfo, text, etc.)
		return
	}
}

func (s *Subscriber) handlePosition(id string, raw json.RawMessage) {
	var p PositionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		slog.Warn("failed to parse position payload", "id", id, "err", err)
		return
	}

	if p.LatitudeI == 0 && p.LongitudeI == 0 {
		slog.Debug("ignoring position with no GPS fix", "id", id)
		return
	}

	lat := float64(p.LatitudeI) * 1e-7
	lon := float64(p.LongitudeI) * 1e-7

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fetch existing device to preserve telemetry fields.
	existing, err := s.queries.GetDevice(ctx, id)
	var batteryLevel int64
	if err == nil {
		batteryLevel = existing.BatteryMv
	}

	_, err = s.queries.UpsertDevice(ctx, db.UpsertDeviceParams{
		ID:        id,
		Lat:       lat,
		Lon:       lon,
		Alt:       p.Altitude,
		Speed:     p.GroundSpeed,
		Course:    0,
		Sats:      p.SatsInView,
		Hdop:      0,
		BatteryMv: batteryLevel,
		Rssi:      0,
		Snr:       0,
		Online:    1,
	})
	if err != nil {
		slog.Error("failed to upsert device position", "id", id, "err", err)
		return
	}

	slog.Info("position updated", "id", id, "lat", lat, "lon", lon, "sats", p.SatsInView)
	s.broadcastDevices(ctx)
}

func (s *Subscriber) handleTelemetry(id string, raw json.RawMessage) {
	var t TelemetryPayload
	if err := json.Unmarshal(raw, &t); err != nil {
		slog.Warn("failed to parse telemetry payload", "id", id, "err", err)
		return
	}

	if t.BatteryLevel == 0 && t.Voltage == 0 {
		// not device telemetry (could be env sensor telemetry — ignore)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Fetch existing device to preserve position fields.
	existing, err := s.queries.GetDevice(ctx, id)
	if err != nil {
		// Device not seen yet — create a placeholder with no location.
		slog.Debug("telemetry for unknown device, creating placeholder", "id", id)
	}

	_, err = s.queries.UpsertDevice(ctx, db.UpsertDeviceParams{
		ID:        id,
		Lat:       existing.Lat,
		Lon:       existing.Lon,
		Alt:       existing.Alt,
		Speed:     existing.Speed,
		Course:    0,
		Sats:      existing.Sats,
		Hdop:      0,
		BatteryMv: int64(t.BatteryLevel),
		Rssi:      0,
		Snr:       0,
		Online:    1,
	})
	if err != nil {
		slog.Error("failed to upsert device telemetry", "id", id, "err", err)
		return
	}

	slog.Info("telemetry updated", "id", id, "battery_level", t.BatteryLevel, "voltage", t.Voltage)
	s.broadcastDevices(ctx)
}

// broadcastDevices sends the full device list to all WebSocket clients.
func (s *Subscriber) broadcastDevices(ctx context.Context) {
	devices, err := s.queries.ListDevices(ctx)
	if err != nil {
		slog.Error("failed to list devices", "err", err)
		return
	}

	views := make([]DeviceView, 0, len(devices))
	for _, d := range devices {
		views = append(views, deviceToView(d))
	}

	msg := DeviceMessage{Type: "devices", Data: views}
	data, err := json.Marshal(msg)
	if err != nil {
		slog.Error("failed to marshal device message", "err", err)
		return
	}

	broadcastCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	s.cm.BroadcastAll(broadcastCtx, data)
}

// StartCleanup runs a background goroutine that removes devices not seen in 48h.
func (s *Subscriber) StartCleanup(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				deleteCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				if err := s.queries.DeleteStaleDevices(deleteCtx); err != nil {
					slog.Error("failed to delete stale devices", "err", err)
				} else {
					s.broadcastDevices(deleteCtx)
				}
				cancel()
			}
		}
	}()
}

// LoadAndBroadcast fetches current devices from DB and returns serialised JSON.
func (s *Subscriber) LoadAndBroadcast(ctx context.Context) ([]byte, error) {
	devices, err := s.queries.ListDevices(ctx)
	if err != nil {
		return nil, err
	}

	views := make([]DeviceView, 0, len(devices))
	for _, d := range devices {
		views = append(views, deviceToView(d))
	}

	msg := DeviceMessage{Type: "devices", Data: views}
	return json.Marshal(msg)
}

// isMeshtasticJSONTopic returns true for topics matching msh/.../2/json/...
func isMeshtasticJSONTopic(topic string) bool {
	parts := strings.Split(topic, "/")
	return len(parts) >= 5 && parts[0] == "msh" && parts[2] == "2" && parts[3] == "json"
}

func deviceToView(d db.Device) DeviceView {
	return DeviceView{
		ID:           d.ID,
		Lat:          d.Lat,
		Lon:          d.Lon,
		Alt:          d.Alt,
		Speed:        d.Speed,
		Sats:         d.Sats,
		BatteryLevel: d.BatteryMv, // stored as battery_level (0-100)
		Online:       d.Online != 0,
		LastSeen:     d.LastSeen.UTC(),
	}
}
