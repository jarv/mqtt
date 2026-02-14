package main

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// ConnectionManager keeps track of active websocket connections.
type ConnectionManager struct {
	connections map[string]connectionInfo
	mutex       sync.RWMutex
}

type connectionInfo struct {
	conns []*websocket.Conn
	name  string
}

func NewConnectionManager() *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]connectionInfo),
	}
}

func (cm *ConnectionManager) Add(name string, conn *websocket.Conn) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	info, exists := cm.connections[name]
	if !exists {
		info = connectionInfo{name: name}
	}
	info.conns = append(info.conns, conn)
	cm.connections[name] = info
}

func (cm *ConnectionManager) Remove(name string, conn *websocket.Conn) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	info, exists := cm.connections[name]
	if !exists {
		return
	}

	for i, c := range info.conns {
		if c == conn {
			info.conns = slices.Delete(info.conns, i, i+1)
			break
		}
	}

	if len(info.conns) == 0 {
		delete(cm.connections, name)
	} else {
		cm.connections[name] = info
	}
}

// BroadcastAll sends a message to all connected clients.
func (cm *ConnectionManager) BroadcastAll(ctx context.Context, message []byte) {
	cm.mutex.RLock()
	var allConns []*websocket.Conn
	var allNames []string
	for _, info := range cm.connections {
		for _, conn := range info.conns {
			allConns = append(allConns, conn)
			allNames = append(allNames, info.name)
		}
	}
	cm.mutex.RUnlock()

	var wg sync.WaitGroup
	for i, conn := range allConns {
		wg.Add(1)
		go func(conn *websocket.Conn, name string) {
			defer wg.Done()
			writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := conn.Write(writeCtx, websocket.MessageText, message); err != nil {
				slog.Warn("broadcast write failed", "client", name, "err", err)
			}
		}(conn, allNames[i])
	}
	wg.Wait()
}

func (cm *ConnectionManager) Count() int {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()
	count := 0
	for _, info := range cm.connections {
		count += len(info.conns)
	}
	return count
}
