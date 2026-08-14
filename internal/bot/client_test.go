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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKIMOClient_CreateInstanceSendsAuthAndBody(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		gotBody = body["template"] + ":" + body["team"]

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"metadata": map[string]string{"name": "web-sqli-team-1"},
			"spec":     map[string]string{"templateRef": "web-sqli", "team": "team-1"},
			"status":   map[string]string{"phase": "Creating"},
		})
	}))
	defer srv.Close()

	c := NewKIMOClient(srv.URL, "secret-key")
	instance, err := c.CreateInstance("web-sqli", "team-1")
	require.NoError(t, err)

	assert.Equal(t, "Bearer secret-key", gotAuth)
	assert.Equal(t, "web-sqli:team-1", gotBody)
	assert.Equal(t, "web-sqli-team-1", instance.Metadata.Name)
	assert.Equal(t, "Creating", instance.Status.Phase)
}

func TestKIMOClient_ListInstancesEncodesFilters(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		json.NewEncoder(w).Encode([]map[string]interface{}{})
	}))
	defer srv.Close()

	c := NewKIMOClient(srv.URL, "secret-key")
	_, err := c.ListInstances("team-1", "web-sqli")
	require.NoError(t, err)

	assert.Equal(t, "challenge=web-sqli&team=team-1", gotQuery)
}

func TestKIMOClient_ReturnsErrorOnNonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "template not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewKIMOClient(srv.URL, "secret-key")
	_, err := c.GetTemplate("missing")
	assert.Error(t, err)
}

func TestKIMOClient_ExtendInstanceSendsTTL(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{})
	}))
	defer srv.Close()

	c := NewKIMOClient(srv.URL, "secret-key")
	err := c.ExtendInstance("web-sqli-team-1", "45m")
	require.NoError(t, err)
	assert.Equal(t, "45m", gotBody["ttl"])
}
