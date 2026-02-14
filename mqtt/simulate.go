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

// simState holds the mutable state for a simulated device.
type simState struct {
	nodeNum     uint32
	latI        int64
	lonI        int64
	altitude    float64
	groundSpeed float64
	satsInView  int64
	battLevel   float64
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
	region := fs.String("region", "EU_868", "Meshtastic region string")
	channel := fs.String("channel", "LongFast", "Meshtastic channel name")

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
		"region", *region,
		"channel", *channel,
	)

	var wg sync.WaitGroup
	for i := range *count {
		wg.Add(1)
		// Use a deterministic fake node number per device index.
		nodeNum := uint32(0xdeadbe00 + i)
		loc := ljubljanaLocations[i%len(ljubljanaLocations)]
		go func(nodeNum uint32, baseLat, baseLon float64) {
			defer wg.Done()
			runDevice(nodeNum, *host, *port, *username, *password, *interval, *region, *channel, baseLat, baseLon)
		}(nodeNum, loc[0], loc[1])
		// Stagger device startups slightly.
		time.Sleep(200 * time.Millisecond)
	}
	wg.Wait()
}

func runDevice(nodeNum uint32, host string, port int, username, password string, interval time.Duration, region, channel string, baseLat, baseLon float64) {
	id := fmt.Sprintf("!%08x", nodeNum)
	broker := fmt.Sprintf("tcp://%s:%d", host, port)

	// Topic: msh/{region}/2/json/{channel}/{node_id}
	topicBase := fmt.Sprintf("msh/%s/2/json/%s/%s", region, channel, id)

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

	state := simState{
		nodeNum:    nodeNum,
		latI:       int64(baseLat * 1e7),
		lonI:       int64(baseLon * 1e7),
		altitude:   12.0,
		satsInView: 8,
		battLevel:  85.0,
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	tick := 0
	for range ticker.C {
		evolveSimState(&state)

		// Alternate between position and telemetry packets.
		var topic string
		var payloadObj any

		if tick%2 == 0 {
			topic = topicBase
			payloadObj = map[string]any{
				"from":      nodeNum,
				"sender":    id,
				"timestamp": time.Now().Unix(),
				"type":      "position",
				"payload": map[string]any{
					"latitude_i":   state.latI,
					"longitude_i":  state.lonI,
					"altitude":     state.altitude,
					"ground_speed": state.groundSpeed,
					"sats_in_view": state.satsInView,
				},
			}
		} else {
			topic = topicBase
			payloadObj = map[string]any{
				"from":      nodeNum,
				"sender":    id,
				"timestamp": time.Now().Unix(),
				"type":      "telemetry",
				"payload": map[string]any{
					"battery_level":       state.battLevel,
					"voltage":             3.3 + state.battLevel/100.0,
					"channel_utilization": 5.0,
					"air_util_tx":         2.0,
				},
			}
		}

		data, err := json.Marshal(payloadObj)
		if err != nil {
			slog.Error("failed to marshal sim payload", "id", id, "err", err)
			tick++
			continue
		}

		tok := client.Publish(topic, 0, false, data)
		tok.Wait()
		if tok.Error() != nil {
			slog.Warn("publish failed", "id", id, "err", tok.Error())
		} else {
			slog.Info("published", "id", id, "type", map[bool]string{true: "position", false: "telemetry"}[tick%2 == 0], "battery", state.battLevel)
		}
		tick++
	}
}

// evolveSimState applies small realistic changes to simulate sensor variation.
func evolveSimState(s *simState) {
	// Battery drains slowly (0.1-0.3% per publish), wraps from 5% back to 100%.
	s.battLevel -= rand.Float64()*0.2 + 0.1
	if s.battLevel < 5 {
		s.battLevel = 100
	}

	// Small position drift (~1-5m per tick)
	s.latI += int64(rand.Float64()*100 - 50)
	s.lonI += int64(rand.Float64()*100 - 50)

	// Satellite count occasionally changes ±1 (6–12 range)
	if rand.IntN(4) == 0 {
		s.satsInView += int64(rand.IntN(3)) - 1
		if s.satsInView < 6 {
			s.satsInView = 6
		}
		if s.satsInView > 12 {
			s.satsInView = 12
		}
	}
}
