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
	"fmt"

	"github.com/bwmarrin/discordgo"
)

// formatTemplatesEmbed renders /challenges list.
func formatTemplatesEmbed(templates []Template) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title: "Challenges",
		Color: 0x5865F2,
	}
	if len(templates) == 0 {
		embed.Description = "No challenges available."
		return embed
	}
	for _, tmpl := range templates {
		status := "not ready"
		if tmpl.Status.Ready {
			status = "ready"
		}
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name: tmpl.Metadata.Name,
			Value: fmt.Sprintf("%s · %s pts · %s · %d instances",
				tmpl.Spec.Category, pointsOrDash(tmpl.Spec.Points), status, tmpl.Status.InstanceCount),
			Inline: false,
		})
	}
	return embed
}

// formatTemplateStatusEmbed renders /challenges status <name>.
func formatTemplateStatusEmbed(tmpl *Template) *discordgo.MessageEmbed {
	status := "not ready"
	if tmpl.Status.Ready {
		status = "ready"
	}
	embed := &discordgo.MessageEmbed{
		Title: tmpl.Metadata.Name,
		Color: 0x5865F2,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Category", Value: valueOrDash(tmpl.Spec.Category), Inline: true},
			{Name: "Difficulty", Value: valueOrDash(tmpl.Spec.Difficulty), Inline: true},
			{Name: "Points", Value: pointsOrDash(tmpl.Spec.Points), Inline: true},
			{Name: "Status", Value: status, Inline: true},
			{Name: "Instances", Value: fmt.Sprintf("%d", tmpl.Status.InstanceCount), Inline: true},
		},
	}
	if tmpl.Status.Message != "" {
		embed.Description = tmpl.Status.Message
	}
	return embed
}

// formatInstanceCreatedEmbed renders /instance create.
func formatInstanceCreatedEmbed(instance *Instance) *discordgo.MessageEmbed {
	return &discordgo.MessageEmbed{
		Title: instance.Metadata.Name,
		Color: 0x57F287,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Challenge", Value: instance.Spec.TemplateRef, Inline: true},
			{Name: "Team", Value: instance.Spec.Team, Inline: true},
			{Name: "Phase", Value: valueOrDash(instance.Status.Phase), Inline: true},
		},
	}
}

// formatInstancesListEmbed renders /instance list.
func formatInstancesListEmbed(instances []Instance) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title: "Instances",
		Color: 0x5865F2,
	}
	if len(instances) == 0 {
		embed.Description = "No matching instances."
		return embed
	}
	for _, inst := range instances {
		endpoint := inst.Status.Endpoint
		if endpoint == "" {
			endpoint = "(not yet exposed)"
		}
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   inst.Metadata.Name,
			Value:  fmt.Sprintf("%s · team %s · %s · %s", inst.Spec.TemplateRef, inst.Spec.Team, inst.Status.Phase, endpoint),
			Inline: false,
		})
	}
	return embed
}

// formatStatsEmbed renders /stats — a dashboard over all templates and instances.
func formatStatsEmbed(templates []Template, instances []Instance) *discordgo.MessageEmbed {
	byPhase := map[string]int{}
	for _, inst := range instances {
		byPhase[inst.Status.Phase]++
	}

	readyCount := 0
	for _, tmpl := range templates {
		if tmpl.Status.Ready {
			readyCount++
		}
	}

	embed := &discordgo.MessageEmbed{
		Title: "KIMO Stats",
		Color: 0x5865F2,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Challenges", Value: fmt.Sprintf("%d (%d ready)", len(templates), readyCount), Inline: true},
			{Name: "Instances", Value: fmt.Sprintf("%d", len(instances)), Inline: true},
		},
	}
	for _, phase := range []string{"Pending", "Creating", "Running", "Unhealthy", "Expiring", "Expired", "Failed"} {
		if count, ok := byPhase[phase]; ok {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name: phase, Value: fmt.Sprintf("%d", count), Inline: true,
			})
		}
	}
	return embed
}

func valueOrDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func pointsOrDash(points int) string {
	if points == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", points)
}
