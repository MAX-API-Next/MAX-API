package openai

import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

const (
	realtimePongWait   = 90 * time.Second
	realtimePingPeriod = 75 * time.Second
	realtimeWriteWait  = 10 * time.Second
)

func configureRealtimeConnection(conn *websocket.Conn, pongWait time.Duration) error {
	if err := conn.SetReadDeadline(time.Now().Add(pongWait)); err != nil {
		return err
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	return nil
}

func runRealtimePingLoop(conn *websocket.Conn, done <-chan struct{}, pingPeriod, writeWait time.Duration, stop func(error)) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				stop(fmt.Errorf("websocket ping failed: %w", err))
				return
			}
		}
	}
}
