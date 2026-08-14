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

package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// EventType identifies a ChallengeInstance lifecycle transition.
type EventType string

const (
	EventCreating  EventType = "instance.creating"
	EventRunning   EventType = "instance.running"
	EventUnhealthy EventType = "instance.unhealthy"
	EventExpiring  EventType = "instance.expiring"
	EventExpired   EventType = "instance.expired"
	EventFailed    EventType = "instance.failed"
	EventDeleted   EventType = "instance.deleted"
)

// Event is dispatched to the active backend on every ChallengeInstance
// phase transition.
type Event struct {
	Type      EventType
	Instance  string
	Challenge string
	Team      string
	Player    string
	Endpoint  string
	Reason    string
	Timestamp time.Time
}

// Principal identifies the caller of a KIMO API request, as resolved by
// the active backend's own auth scheme.
type Principal struct {
	Subject string
	Team    string
	Scopes  []string
}

// Backend is implemented by every scoring-platform integration. It is the
// single seam between KIMO's controllers/API and an external platform.
type Backend interface {
	Name() string
	Notify(ctx context.Context, event Event) error
	Authenticate(r *http.Request) (Principal, error)
}

// Factory constructs a Backend from its opaque, backend-specific config.
type Factory func(cfg json.RawMessage) (Backend, error)

var registry = map[string]Factory{}

// Register makes a backend available by name. Called from each adapter's
// init() — this is how a new integration becomes selectable without
// touching any controller or API code.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// New constructs the named backend from config. Returns an error if no
// backend was registered under that name.
func New(name string, cfg json.RawMessage) (Backend, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown scoring backend %q", name)
	}
	return factory(cfg)
}
