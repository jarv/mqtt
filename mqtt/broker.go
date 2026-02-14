package main

import (
	"log/slog"

	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"
)

// authHook wraps auth.Hook to log failed authentication attempts.
type authHook struct {
	auth.Hook
}

func (h *authHook) OnConnectAuthenticate(cl *mqtt.Client, pk packets.Packet) bool {
	ok := h.Hook.OnConnectAuthenticate(cl, pk)
	if !ok {
		slog.Warn("MQTT authentication failed", "username", string(pk.Connect.Username), "remote", cl.Net.Remote)
	}
	return ok
}

// Broker wraps the mochi-mqtt server.
type Broker struct {
	server   *mqtt.Server
	addr     string
	username string
	password string
}

func NewBroker(addr, username, password string) *Broker {
	return &Broker{
		addr:     addr,
		username: username,
		password: password,
	}
}

// Start initializes and starts the embedded MQTT broker.
func (b *Broker) Start(onPublish func(topic string, payload []byte)) error {
	b.server = mqtt.New(&mqtt.Options{
		InlineClient: true,
	})

	// Auth hook — accept only connections with the configured credentials.
	if err := b.server.AddHook(new(authHook), &auth.Options{
		Ledger: &auth.Ledger{
			Auth: auth.AuthRules{
				{Username: auth.RString(b.username), Password: auth.RString(b.password), Allow: true},
			},
			ACL: auth.ACLRules{
				{Username: auth.RString(b.username), Filters: auth.Filters{"msh/#": auth.ReadWrite}},
			},
		},
	}); err != nil {
		return err
	}

	// TCP listener on the configured address.
	tcp := listeners.NewTCP(listeners.Config{ID: "tcp", Address: b.addr})
	if err := b.server.AddListener(tcp); err != nil {
		return err
	}

	// Subscribe inline to all Meshtastic JSON topics.
	if err := b.server.Subscribe("msh/+/2/json/#", 1, func(_ *mqtt.Client, _ packets.Subscription, pk packets.Packet) {
		onPublish(pk.TopicName, pk.Payload)
	}); err != nil {
		return err
	}

	go func() {
		if err := b.server.Serve(); err != nil {
			slog.Error("MQTT broker error", "err", err)
		}
	}()

	slog.Info("MQTT broker started", "addr", b.addr)
	return nil
}

// Stop gracefully shuts down the broker.
func (b *Broker) Stop() error {
	if b.server != nil {
		return b.server.Close()
	}
	return nil
}
