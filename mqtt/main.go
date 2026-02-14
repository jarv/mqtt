package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"os"
	"time"

	"github.com/jarv/mqtt/db"
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

func main() {
	// Subcommand dispatch
	if len(os.Args) > 1 && os.Args[1] == "simulate" {
		runSimulate(os.Args[2:])
		return
	}

	runServe(os.Args[1:])
}

func runServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", "localhost:8910", "HTTP server address")
	mqttAddr := fs.String("mqtt-addr", ":1883", "MQTT broker address")
	dbPath := fs.String("db", ":memory:", "SQLite database path (default: in-memory)")
	jsonLog := fs.Bool("json", false, "use JSON logging")

	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Credentials from environment
	mqttUsername := os.Getenv("MQTT_USERNAME")
	if mqttUsername == "" {
		mqttUsername = "devices"
	}
	mqttPassword := os.Getenv("MQTT_PASSWORD")
	if mqttPassword == "" {
		slog.Error("MQTT_PASSWORD environment variable is required")
		os.Exit(1)
	}

	// Logging setup
	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if *jsonLog {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = &MultilineHandler{Writer: os.Stdout}
	}
	slog.SetDefault(slog.New(handler))

	// Open SQLite database
	sqlDB, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		slog.Error("failed to open database", "err", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	// Run schema migrations
	if _, err := sqlDB.Exec(schema); err != nil {
		slog.Error("failed to apply schema", "err", err)
		os.Exit(1)
	}

	queries := db.New(sqlDB)
	cm := NewConnectionManager()
	sub := NewSubscriber(queries, cm)

	// Start background cleanup — removes devices unseen for 48h, checks every 15 minutes
	sub.StartCleanup(context.Background(), 15*time.Minute)

	// Start embedded MQTT broker
	broker := NewBroker(*mqttAddr, mqttUsername, mqttPassword)
	if err := broker.Start(sub.HandleMessage); err != nil {
		slog.Error("failed to start MQTT broker", "err", err)
		os.Exit(1)
	}
	defer broker.Stop()

	// Start HTTP server (blocks)
	app := NewApp(*addr, cm, sub)
	if err := app.Run(); err != nil {
		slog.Error("HTTP server error", "err", err)
		os.Exit(1)
	}
}

// schema is the DDL run at startup to ensure the table exists.
const schema = `
CREATE TABLE IF NOT EXISTS devices (
    id          TEXT PRIMARY KEY,
    lat         REAL NOT NULL DEFAULT 0,
    lon         REAL NOT NULL DEFAULT 0,
    alt         REAL NOT NULL DEFAULT 0,
    speed       REAL NOT NULL DEFAULT 0,
    course      REAL NOT NULL DEFAULT 0,
    sats        INTEGER NOT NULL DEFAULT 0,
    hdop        REAL NOT NULL DEFAULT 0,
    battery_mv  INTEGER NOT NULL DEFAULT 0,
    rssi        REAL NOT NULL DEFAULT 0,
    snr         REAL NOT NULL DEFAULT 0,
    online      INTEGER NOT NULL DEFAULT 1,
    last_seen   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`
