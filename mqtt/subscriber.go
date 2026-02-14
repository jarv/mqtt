package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/jarv/mqtt/db"
)

// DevicePayload is the JSON payload published by devices.
type DevicePayload struct {
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Alt       float64 `json:"alt"`
	Speed     float64 `json:"speed"`
	Course    float64 `json:"course"`
	Sats      int64   `json:"sats"`
	Hdop      float64 `json:"hdop"`
	BatteryMv int64   `json:"battery_mv"`
	Rssi      float64 `json:"rssi"`
	Snr       float64 `json:"snr"`
}

// DeviceMessage is sent over WebSocket to browsers.
type DeviceMessage struct {
	Type string       `json:"type"`
	Data []DeviceView `json:"data"`
}

// DeviceView is the browser-facing representation of a device.
type DeviceView struct {
	ID        string    `json:"id"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Alt       float64   `json:"alt"`
	Speed     float64   `json:"speed"`
	Course    float64   `json:"course"`
	Sats      int64     `json:"sats"`
	Hdop      float64   `json:"hdop"`
	BatteryMv int64     `json:"battery_mv"`
	Rssi      float64   `json:"rssi"`
	Snr       float64   `json:"snr"`
	Online    bool      `json:"online"`
	LastSeen  time.Time `json:"last_seen"`
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
	deviceID := extractDeviceID(topic)
	if deviceID == "" {
		slog.Warn("could not extract device ID from topic", "topic", topic)
		return
	}

	var p DevicePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		slog.Warn("failed to parse device payload", "topic", topic, "err", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.queries.UpsertDevice(ctx, db.UpsertDeviceParams{
		ID:        deviceID,
		Lat:       p.Lat,
		Lon:       p.Lon,
		Alt:       p.Alt,
		Speed:     p.Speed,
		Course:    p.Course,
		Sats:      p.Sats,
		Hdop:      p.Hdop,
		BatteryMv: p.BatteryMv,
		Rssi:      p.Rssi,
		Snr:       p.Snr,
		Online:    1,
	})
	if err != nil {
		slog.Error("failed to upsert device", "id", deviceID, "err", err)
		return
	}

	slog.Info("device updated", "id", deviceID, "lat", p.Lat, "lon", p.Lon)
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

// StartCleanup runs a background goroutine that removes devices not seen in 48h
// and broadcasts the updated list to all connected clients.
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

func extractDeviceID(topic string) string {
	// Expected format: devices/{id}/status
	parts := strings.Split(topic, "/")
	if len(parts) == 3 && parts[0] == "devices" && parts[2] == "status" {
		return parts[1]
	}
	return ""
}

func deviceToView(d db.Device) DeviceView {
	return DeviceView{
		ID:        d.ID,
		Lat:       d.Lat,
		Lon:       d.Lon,
		Alt:       d.Alt,
		Speed:     d.Speed,
		Course:    d.Course,
		Sats:      d.Sats,
		Hdop:      d.Hdop,
		BatteryMv: d.BatteryMv,
		Rssi:      d.Rssi,
		Snr:       d.Snr,
		Online:    d.Online != 0,
		LastSeen:  d.LastSeen.UTC(),
	}
}
