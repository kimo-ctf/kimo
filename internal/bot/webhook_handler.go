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

	"github.com/bwmarrin/discordgo"
)

func (m *Monitor) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	var event WebhookEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	channelID, active := m.target()
	if active && channelID != "" {
		embed := formatEvent(event)
		_, _ = m.session.ChannelMessageSendEmbed(channelID, embed)
	}
	w.WriteHeader(http.StatusOK)
}

func formatEvent(event WebhookEvent) *discordgo.MessageEmbed {
	color := 0x57F287 // green for running
	switch event.Event {
	case "instance.failed":
		color = 0xED4245
	case "instance.unhealthy":
		color = 0xFEE75C
	case "instance.expiring", "instance.expired":
		color = 0xFEE75C
	case "instance.creating":
		color = 0x5865F2
	}

	fields := []*discordgo.MessageEmbedField{
		{Name: "Challenge", Value: valueOrDash(event.Challenge), Inline: true},
		{Name: "Team", Value: valueOrDash(event.Team), Inline: true},
	}
	if event.Endpoint != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Endpoint", Value: event.Endpoint, Inline: false})
	}
	if event.Reason != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Reason", Value: event.Reason, Inline: false})
	}

	return &discordgo.MessageEmbed{
		Title:       event.Instance,
		Description: event.Event,
		Color:       color,
		Fields:      fields,
	}
}
