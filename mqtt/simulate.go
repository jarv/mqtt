package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

// simPayload matches the DevicePayload structure published by real devices.
type simPayload struct {
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Alt       float64 `json:"alt"`
	Speed     float64 `json:"speed"`
	Course    float64 `json:"course"`
	Sats      int     `json:"sats"`
	Hdop      float64 `json:"hdop"`
	BatteryMv int     `json:"battery_mv"`
	Rssi      float64 `json:"rssi"`
	Snr       float64 `json:"snr"`
}

func runSimulate(args []string) {
	// Ljubljana bounding box — random locations within the city centre
	ljubljanaLocations := [][2]float64{
		{46.0569, 14.5058}, // Congress Square
		{46.0512, 14.5060}, // Ljubljana Castle area
		{46.0490, 14.5036}, // Tivoli Park
		{46.0546, 14.5144}, // BTC City
		{46.0623, 14.5100}, // Šiška
		{46.0435, 14.4986}, // Vič
		{46.0551, 14.4915}, // Rožna dolina
		{46.0604, 14.5225}, // Bežigrad
		{46.0478, 14.5188}, // Moste
		{46.0530, 14.5010}, // Ajdovščina
	}

	fs := flag.NewFlagSet("simulate", flag.ExitOnError)
	host := fs.String("host", "localhost", "MQTT broker host")
	port := fs.Int("port", 1883, "MQTT broker port")
	username := fs.String("username", "devices", "MQTT username")
	password := fs.String("password", "", "MQTT password")
	count := fs.Int("count", 1, "Number of simulated devices")
	interval := fs.Duration("interval", 5*time.Second, "Publish interval per device")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if *password == "" {
		fmt.Fprintln(os.Stderr, "error: --password is required for simulate")
		fs.Usage()
		os.Exit(1)
	}

	slog.Info("starting simulator",
		"count", *count,
		"host", *host,
		"port", *port,
		"interval", *interval,
	)

	var wg sync.WaitGroup
	for i := range *count {
		wg.Add(1)
		deviceID := fmt.Sprintf("sim-device-%03d", i+1)
		loc := ljubljanaLocations[i%len(ljubljanaLocations)]
		go func(id string, baseLat, baseLon float64) {
			defer wg.Done()
			runDevice(id, *host, *port, *username, *password, *interval, baseLat, baseLon)
		}(deviceID, loc[0], loc[1])
		// Stagger device startups slightly
		time.Sleep(200 * time.Millisecond)
	}
	wg.Wait()
}

func runDevice(id, host string, port int, username, password string, interval time.Duration, baseLat, baseLon float64) {
	broker := fmt.Sprintf("tcp://%s:%d", host, port)
	topic := fmt.Sprintf("devices/%s/status", id)

	opts := pahomqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(id).
		SetUsername(username).
		SetPassword(password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetOnConnectHandler(func(_ pahomqtt.Client) {
			slog.Info("simulator device connected", "id", id)
		}).
		SetConnectionLostHandler(func(_ pahomqtt.Client, err error) {
			slog.Warn("simulator device disconnected", "id", id, "err", err)
		})

	client := pahomqtt.NewClient(opts)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		slog.Error("simulator device failed to connect", "id", id, "err", tok.Error())
		return
	}
	defer client.Disconnect(250)

	// Initial state — realistic ESP32 LoRa V4 + GNSS values.
	state := simPayload{
		Lat:       baseLat,
		Lon:       baseLon,
		Alt:       12.0,
		Speed:     0.0,
		Course:    0.0,
		Sats:      8,
		Hdop:      1.2,
		BatteryMv: 3900,
		Rssi:      -85.0,
		Snr:       7.0,
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		evolveState(&state)

		payload, err := json.Marshal(state)
		if err != nil {
			slog.Error("failed to marshal sim payload", "id", id, "err", err)
			continue
		}

		tok := client.Publish(topic, 0, false, payload)
		tok.Wait()
		if tok.Error() != nil {
			slog.Warn("publish failed", "id", id, "err", tok.Error())
		} else {
			slog.Info("published", "id", id, "battery_mv", state.BatteryMv, "rssi", state.Rssi)
		}
	}
}

// evolveState applies small realistic changes to simulate sensor variation.
func evolveState(s *simPayload) {
	// Battery drains slowly (1–3 mV per publish), wraps from 3300 back to 4200.
	s.BatteryMv -= rand.IntN(3) + 1
	if s.BatteryMv < 3300 {
		s.BatteryMv = 4200
	}

	// RSSI jitter ±3 dBm
	s.Rssi += (rand.Float64()*6 - 3)
	s.Rssi = clamp(s.Rssi, -120, -60)

	// SNR jitter ±0.5
	s.Snr += (rand.Float64() - 0.5)
	s.Snr = clamp(s.Snr, 2, 12)

	// Satellite count occasionally changes ±1 (6–12 range)
	if rand.IntN(4) == 0 {
		s.Sats += rand.IntN(3) - 1
		if s.Sats < 6 {
			s.Sats = 6
		}
		if s.Sats > 12 {
			s.Sats = 12
		}
	}

	// HDOP small jitter
	s.Hdop += (rand.Float64()*0.4 - 0.2)
	s.Hdop = clamp(s.Hdop, 0.8, 2.5)
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
