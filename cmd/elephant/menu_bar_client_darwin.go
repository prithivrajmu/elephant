//go:build darwin

package main

import (
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	memory "github.com/prithivrajmu/elephant"
)

// Only these protocol metadata fields cross the local companion socket.
// A snapshot retains initialize even if several calls finish before attachment.
type menuBarObservation struct {
	Initialized bool                 `json:"initialized"`
	Client      memory.MCPClientInfo `json:"client"`
	Tool        string               `json:"tool,omitempty"`
	At          time.Time            `json:"at,omitempty"`
	Failed      bool                 `json:"failed,omitempty"`
}

func cleanMenuText(value string, limit int) string {
	value = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value))
	if utf8.RuneCountInString(value) > limit {
		n := 0
		for i := range value {
			if n == limit {
				return value[:i] + "…"
			}
			n++
		}
	}
	return value
}

func (o menuBarObservation) clean() menuBarObservation {
	o.Client.Name = cleanMenuText(o.Client.Name, 80)
	o.Client.Title = cleanMenuText(o.Client.Title, 80)
	o.Client.Version = cleanMenuText(o.Client.Version, 48)
	o.Tool = cleanMenuText(o.Tool, 64)
	return o
}

type menuBarObservations struct {
	mu      sync.Mutex
	value   menuBarObservation
	changed chan struct{}
}

func (o *menuBarObservations) observe(event memory.MCPEvent) {
	o.mu.Lock()
	if event.Tool == "" {
		o.value.Initialized, o.value.Client = true, event.Client
	} else {
		o.value.Tool, o.value.At, o.value.Failed = event.Tool, event.At, event.Failed
	}
	o.value = o.value.clean()
	o.mu.Unlock()
	select {
	case o.changed <- struct{}{}:
	default:
	}
}

func (o *menuBarObservations) snapshot() menuBarObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.value
}

func (c menuBarClient) serve(svc memory.Service, kind string, serve func(memory.Service) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observations := &menuBarObservations{changed: make(chan struct{}, 1)}
	previous := svc.ObserveMCP
	svc.ObserveMCP = func(event memory.MCPEvent) {
		if previous != nil {
			previous(event)
		}
		observations.observe(event)
	}
	if kind == "palace" {
		observations.observe(memory.MCPEvent{Client: memory.MCPClientInfo{Name: "elephant-palace", Title: "Memory Palace", Version: memory.Version}})
	}
	session := newMenuBarSession(svc)
	session.Kind = kind
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.hold(ctx, session, observations)
	}()
	err := serve(svc)
	cancel()
	<-done
	return err
}

// Hide or host EOF closes this registration permanently. It does not reconnect
// or change the lifetime of the MCP/dashboard function running in the caller.
func (c menuBarClient) hold(ctx context.Context, session menuBarSession, observations *menuBarObservations) {
	logPath := filepath.Join(c.dir, menuBarLogName)
	conn, reader, err := c.attach(ctx, session)
	if err != nil {
		if ctx.Err() == nil {
			c.warn("menu bar unavailable: %v (log: %s)", err, logPath)
		}
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	written := make(chan struct{})
	go func() {
		defer close(written)
		writeMenuBarObservations(ctx, conn, observations)
	}()
	defer func() {
		cancel()
		conn.Close()
		stop()
		<-written
	}()
	for {
		var reply menuBarReply
		if readMenuBarLine(reader, &reply) != nil {
			return
		}
		if reply.Error != "" && ctx.Err() == nil {
			c.warn("menu bar stopped: %s (log: %s)", reply.Error, logPath)
		}
	}
}

func writeMenuBarObservations(ctx context.Context, conn net.Conn, observations *menuBarObservations) {
	encoder := json.NewEncoder(conn)
	for {
		select {
		case <-ctx.Done():
			return
		case <-observations.changed:
			if err := encoder.Encode(observations.snapshot()); err != nil {
				conn.Close()
				return
			}
		}
	}
}
