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
	"testing"

	"github.com/bwmarrin/discordgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatTemplatesEmbed_Empty(t *testing.T) {
	embed := formatTemplatesEmbed(nil)
	assert.Equal(t, "No challenges available.", embed.Description)
}

func TestFormatTemplatesEmbed_ListsEachTemplate(t *testing.T) {
	templates := []Template{
		{Metadata: struct {
			Name string `json:"name"`
		}{Name: "web-sqli"},
			Spec: struct {
				Category   string `json:"category"`
				Difficulty string `json:"difficulty"`
				Points     int    `json:"points"`
			}{Category: "web", Points: 100},
			Status: struct {
				Ready         bool   `json:"ready"`
				InstanceCount int    `json:"instanceCount"`
				Message       string `json:"message"`
			}{Ready: true, InstanceCount: 3},
		},
	}
	embed := formatTemplatesEmbed(templates)
	require.Len(t, embed.Fields, 1)
	assert.Equal(t, "web-sqli", embed.Fields[0].Name)
	assert.Contains(t, embed.Fields[0].Value, "ready")
	assert.Contains(t, embed.Fields[0].Value, "3 instances")
}

func TestFormatTemplateStatusEmbed_NotReadyShowsMessage(t *testing.T) {
	tmpl := &Template{}
	tmpl.Metadata.Name = "broken"
	tmpl.Status.Ready = false
	tmpl.Status.Message = "flag secret not found"

	embed := formatTemplateStatusEmbed(tmpl)
	assert.Equal(t, "broken", embed.Title)
	assert.Equal(t, "flag secret not found", embed.Description)

	var statusField *discordgo.MessageEmbedField
	for _, f := range embed.Fields {
		if f.Name == "Status" {
			statusField = f
		}
	}
	require.NotNil(t, statusField)
	assert.Equal(t, "not ready", statusField.Value)
}

func TestFormatInstanceCreatedEmbed(t *testing.T) {
	instance := &Instance{}
	instance.Metadata.Name = "web-sqli-team-1"
	instance.Spec.TemplateRef = "web-sqli"
	instance.Spec.Team = "team-1"
	instance.Status.Phase = "Creating"

	embed := formatInstanceCreatedEmbed(instance)
	assert.Equal(t, "web-sqli-team-1", embed.Title)
	assert.Equal(t, 0x57F287, embed.Color)
}

func TestFormatInstancesListEmbed_Empty(t *testing.T) {
	embed := formatInstancesListEmbed(nil)
	assert.Equal(t, "No matching instances.", embed.Description)
}

func TestFormatInstancesListEmbed_ShowsEndpointPlaceholderWhenMissing(t *testing.T) {
	inst := Instance{}
	inst.Metadata.Name = "web-sqli-team-1"
	inst.Spec.TemplateRef = "web-sqli"
	inst.Spec.Team = "team-1"
	inst.Status.Phase = "Creating"

	embed := formatInstancesListEmbed([]Instance{inst})
	require.Len(t, embed.Fields, 1)
	assert.Contains(t, embed.Fields[0].Value, "(not yet exposed)")
}

func TestFormatStatsEmbed_CountsByPhase(t *testing.T) {
	templates := []Template{{}, {}}
	templates[0].Status.Ready = true

	instances := []Instance{{}, {}, {}}
	instances[0].Status.Phase = "Running"
	instances[1].Status.Phase = "Running"
	instances[2].Status.Phase = "Creating"

	embed := formatStatsEmbed(templates, instances)

	values := map[string]string{}
	for _, f := range embed.Fields {
		values[f.Name] = f.Value
	}
	assert.Equal(t, "2 (1 ready)", values["Challenges"])
	assert.Equal(t, "3", values["Instances"])
	assert.Equal(t, "2", values["Running"])
	assert.Equal(t, "1", values["Creating"])
}

func TestFormatEvent_ColorsByType(t *testing.T) {
	running := formatEvent(WebhookEvent{Event: "instance.running", Instance: "web-sqli-team-1"})
	assert.Equal(t, 0x57F287, running.Color)

	failed := formatEvent(WebhookEvent{Event: "instance.failed", Instance: "web-sqli-team-1"})
	assert.Equal(t, 0xED4245, failed.Color)

	assert.Equal(t, "web-sqli-team-1", running.Title)
	assert.Equal(t, "instance.running", running.Description)
}

func TestFormatEvent_IncludesEndpointAndReasonWhenPresent(t *testing.T) {
	event := WebhookEvent{
		Event:     "instance.unhealthy",
		Instance:  "web-sqli-team-1",
		Endpoint:  "1.2.3.4:8080",
		Reason:    "pod failing readiness checks",
		Challenge: "web-sqli",
		Team:      "team-1",
	}
	embed := formatEvent(event)

	values := map[string]string{}
	for _, f := range embed.Fields {
		values[f.Name] = f.Value
	}
	assert.Equal(t, "1.2.3.4:8080", values["Endpoint"])
	assert.Equal(t, "pod failing readiness checks", values["Reason"])
}
