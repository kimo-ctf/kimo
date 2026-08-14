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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMonitor_SetTargetUpdatesState(t *testing.T) {
	m := &Monitor{}
	channelID, active := m.target()
	assert.Empty(t, channelID)
	assert.False(t, active)

	m.SetTarget("chan-1", true)
	channelID, active = m.target()
	assert.Equal(t, "chan-1", channelID)
	assert.True(t, active)

	m.SetTarget("", false)
	channelID, active = m.target()
	assert.Empty(t, channelID)
	assert.False(t, active)
}

func TestMonitor_HandleWebhook_RejectsBadJSON(t *testing.T) {
	m := &Monitor{}
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("not json"))
	w := httptest.NewRecorder()

	m.HandleWebhook(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMonitor_HandleWebhook_InactiveDoesNotTouchSession(t *testing.T) {
	m := &Monitor{} // session is nil — a call into it would panic if reached
	body, _ := json.Marshal(WebhookEvent{Event: "instance.running", Instance: "web-sqli-team-1"})
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	w := httptest.NewRecorder()

	require.NotPanics(t, func() { m.HandleWebhook(w, req) })
	assert.Equal(t, http.StatusOK, w.Code)
}
