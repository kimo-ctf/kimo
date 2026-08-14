/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bot

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Monitor runs a small HTTP server that receives lifecycle events from the
// generic scoring backend's webhook fan-out and posts them to a Discord
// channel. When a different backend (e.g. ctfd) is active, event streaming
// instead comes from that platform's own tooling — /monitor documents this.
type Monitor struct {
	session *discordgo.Session
	server  *http.Server

	mu        sync.RWMutex
	channelID string
	active    bool
}

// WebhookEvent mirrors the JSON payload posted by
// internal/integrations.genericBackend.Notify (see internal/integrations/backend.go
// for the Event type it's marshaled from).
type WebhookEvent struct {
	Event     string    `json:"type"`
	Instance  string    `json:"instance"`
	Challenge string    `json:"challenge"`
	Team      string    `json:"team"`
	Player    string    `json:"player,omitempty"`
	Endpoint  string    `json:"endpoint,omitempty"`
	Reason    string    `json:"reason,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// Start begins listening for webhook POSTs on addr. It does not block.
func (m *Monitor) Start(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", m.HandleWebhook)
	m.server = &http.Server{Addr: addr, Handler: mux}
	go m.server.ListenAndServe() //nolint:errcheck // logged via ListenAndServe's own stderr output
}

func (m *Monitor) Stop() {
	if m.server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.server.Shutdown(ctx)
}

// SetTarget points the monitor at a channel and toggles whether events are
// actually posted there. Called by the /monitor start and /monitor stop
// command handlers.
func (m *Monitor) SetTarget(channelID string, active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channelID = channelID
	m.active = active
}

func (m *Monitor) target() (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.channelID, m.active
}
