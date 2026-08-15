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
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubBackend struct{ name string }

func (s *stubBackend) Name() string                                  { return s.name }
func (s *stubBackend) Notify(context.Context, Event) error           { return nil }
func (s *stubBackend) Authenticate(*http.Request) (Principal, error) { return Principal{}, nil }

func TestRegistry_RegisterAndNew(t *testing.T) {
	Register("stub-for-test", func(cfg json.RawMessage) (Backend, error) {
		return &stubBackend{name: "stub-for-test"}, nil
	})

	b, err := New("stub-for-test", nil)
	require.NoError(t, err)
	assert.Equal(t, "stub-for-test", b.Name())
}

func TestRegistry_UnknownBackend(t *testing.T) {
	_, err := New("does-not-exist", nil)
	assert.Error(t, err)
}
