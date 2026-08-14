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

// Package bot implements a Discord bot that drives the KIMO REST API.
package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Template and Instance are deliberately narrow, hand-written DTOs rather
// than the api/v1alpha1 CRD types — the bot only ever talks to KIMO over
// its REST API, never to the Kubernetes API directly, so it has no
// business depending on k8s.io/* packages.
type Template struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Category   string `json:"category"`
		Difficulty string `json:"difficulty"`
		Points     int    `json:"points"`
	} `json:"spec"`
	Status struct {
		Ready         bool   `json:"ready"`
		InstanceCount int    `json:"instanceCount"`
		Message       string `json:"message"`
	} `json:"status"`
}

type Instance struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		TemplateRef string `json:"templateRef"`
		Team        string `json:"team"`
		Player      string `json:"player"`
	} `json:"spec"`
	Status struct {
		Phase    string `json:"phase"`
		Reason   string `json:"reason"`
		Endpoint string `json:"endpoint"`
		Message  string `json:"message"`
	} `json:"status"`
}

// KIMOClient is a thin HTTP client for the KIMO REST API.
type KIMOClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewKIMOClient(baseURL, apiKey string) *KIMOClient {
	return &KIMOClient{
		baseURL: baseURL,
		apiKey:  apiKey,
		http:    &http.Client{},
	}
}

func (c *KIMOClient) do(method, path string, body interface{}) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling kimo api: %w", err)
	}
	return resp, nil
}

// decodeInto reads resp, decoding into v (if non-nil) on success and
// returning an error describing the body on any non-2xx status.
func decodeInto(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("kimo api returned %d: %s", resp.StatusCode, string(body))
	}
	if v == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func (c *KIMOClient) ListTemplates() ([]Template, error) {
	resp, err := c.do(http.MethodGet, "/api/v1/templates", nil)
	if err != nil {
		return nil, err
	}
	var templates []Template
	if err := decodeInto(resp, &templates); err != nil {
		return nil, err
	}
	return templates, nil
}

func (c *KIMOClient) GetTemplate(name string) (*Template, error) {
	resp, err := c.do(http.MethodGet, "/api/v1/templates/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	var tmpl Template
	if err := decodeInto(resp, &tmpl); err != nil {
		return nil, err
	}
	return &tmpl, nil
}

func (c *KIMOClient) CreateInstance(template, team string) (*Instance, error) {
	resp, err := c.do(http.MethodPost, "/api/v1/instances", map[string]string{
		"template": template,
		"team":     team,
	})
	if err != nil {
		return nil, err
	}
	var instance Instance
	if err := decodeInto(resp, &instance); err != nil {
		return nil, err
	}
	return &instance, nil
}

func (c *KIMOClient) ListInstances(team, challenge string) ([]Instance, error) {
	q := url.Values{}
	if team != "" {
		q.Set("team", team)
	}
	if challenge != "" {
		q.Set("challenge", challenge)
	}
	path := "/api/v1/instances"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	resp, err := c.do(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var instances []Instance
	if err := decodeInto(resp, &instances); err != nil {
		return nil, err
	}
	return instances, nil
}

func (c *KIMOClient) DeleteInstance(name string) error {
	resp, err := c.do(http.MethodDelete, "/api/v1/instances/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	return decodeInto(resp, nil)
}

func (c *KIMOClient) ExtendInstance(name, duration string) error {
	resp, err := c.do(http.MethodPatch, "/api/v1/instances/"+url.PathEscape(name)+"/extend", map[string]string{
		"ttl": duration,
	})
	if err != nil {
		return err
	}
	return decodeInto(resp, nil)
}
