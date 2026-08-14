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

var commandDefinitions = []*discordgo.ApplicationCommand{
	{
		Name:        "challenges",
		Description: "View CTF challenges",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "list", Description: "List all challenges"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "status", Description: "Show a challenge's status", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Challenge name", Required: true},
			}},
		},
	},
	{
		Name:        "instance",
		Description: "Manage challenge instances",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "create", Description: "Launch an instance", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "template", Description: "Challenge template name", Required: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "team", Description: "Team name", Required: true},
			}},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "destroy", Description: "Tear down an instance", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Instance name", Required: true},
			}},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "extend", Description: "Extend an instance's TTL", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "name", Description: "Instance name", Required: true},
				{Type: discordgo.ApplicationCommandOptionString, Name: "duration", Description: "Extra time, e.g. 30m", Required: true},
			}},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "list", Description: "List instances", Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "team", Description: "Filter by team", Required: false},
				{Type: discordgo.ApplicationCommandOptionString, Name: "challenge", Description: "Filter by challenge", Required: false},
			}},
		},
	},
	{
		Name:        "stats",
		Description: "Show a KIMO dashboard",
	},
	{
		Name:        "monitor",
		Description: "Control live event monitoring in this channel",
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "start", Description: "Start posting lifecycle events to this channel"},
			{Type: discordgo.ApplicationCommandOptionSubCommand, Name: "stop", Description: "Stop posting lifecycle events"},
		},
	},
}

// mutatingCommands require the Organizer (or Admin) role: they change
// cluster state. Read-only commands are open to anyone in the server.
var mutatingCommands = map[string]bool{
	"instance.create":  true,
	"instance.destroy": true,
	"instance.extend":  true,
	"monitor.start":    true,
	"monitor.stop":     true,
}

func (b *Bot) registerHandlers() {
	b.session.AddHandler(b.handleInteraction)
}

// registerCommands publishes commandDefinitions as global Discord slash
// commands. Called once, after the session is open.
func (b *Bot) registerCommands() error {
	for _, cmd := range commandDefinitions {
		if _, err := b.session.ApplicationCommandCreate(b.session.State.User.ID, "", cmd); err != nil {
			return fmt.Errorf("registering command %q: %w", cmd.Name, err)
		}
	}
	return nil
}

func (b *Bot) handleInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	data := i.ApplicationCommandData()
	sub := data.Options[0]
	key := data.Name + "." + sub.Name

	if mutatingCommands[key] && !b.isOrganizer(i.Member) {
		b.respond(s, i, "You need the organizer role to run this command.")
		return
	}

	switch key {
	case "challenges.list":
		b.handleChallengesList(s, i)
	case "challenges.status":
		b.handleChallengesStatus(s, i, sub)
	case "instance.create":
		b.handleInstanceCreate(s, i, sub)
	case "instance.destroy":
		b.handleInstanceDestroy(s, i, sub)
	case "instance.extend":
		b.handleInstanceExtend(s, i, sub)
	case "instance.list":
		b.handleInstanceList(s, i, sub)
	case "monitor.start":
		b.handleMonitorStart(s, i)
	case "monitor.stop":
		b.handleMonitorStop(s, i)
	default:
		if data.Name == "stats" {
			b.handleStats(s, i)
		}
	}
}

func (b *Bot) handleChallengesList(s *discordgo.Session, i *discordgo.InteractionCreate) {
	templates, err := b.kimo.ListTemplates()
	if err != nil {
		b.respond(s, i, "Error: "+err.Error())
		return
	}
	b.respondEmbed(s, i, formatTemplatesEmbed(templates))
}

func (b *Bot) handleChallengesStatus(s *discordgo.Session, i *discordgo.InteractionCreate, sub *discordgo.ApplicationCommandInteractionDataOption) {
	name := stringOption(sub, "name")
	tmpl, err := b.kimo.GetTemplate(name)
	if err != nil {
		b.respond(s, i, "Error: "+err.Error())
		return
	}
	b.respondEmbed(s, i, formatTemplateStatusEmbed(tmpl))
}

func (b *Bot) handleInstanceCreate(s *discordgo.Session, i *discordgo.InteractionCreate, sub *discordgo.ApplicationCommandInteractionDataOption) {
	template := stringOption(sub, "template")
	team := stringOption(sub, "team")
	instance, err := b.kimo.CreateInstance(template, team)
	if err != nil {
		b.respond(s, i, "Error: "+err.Error())
		return
	}
	b.respondEmbed(s, i, formatInstanceCreatedEmbed(instance))
}

func (b *Bot) handleInstanceDestroy(s *discordgo.Session, i *discordgo.InteractionCreate, sub *discordgo.ApplicationCommandInteractionDataOption) {
	name := stringOption(sub, "name")
	if err := b.kimo.DeleteInstance(name); err != nil {
		b.respond(s, i, "Error: "+err.Error())
		return
	}
	b.respond(s, i, fmt.Sprintf("Destroyed instance %q.", name))
}

func (b *Bot) handleInstanceExtend(s *discordgo.Session, i *discordgo.InteractionCreate, sub *discordgo.ApplicationCommandInteractionDataOption) {
	name := stringOption(sub, "name")
	duration := stringOption(sub, "duration")
	if err := b.kimo.ExtendInstance(name, duration); err != nil {
		b.respond(s, i, "Error: "+err.Error())
		return
	}
	b.respond(s, i, fmt.Sprintf("Extended instance %q by %s.", name, duration))
}

func (b *Bot) handleInstanceList(s *discordgo.Session, i *discordgo.InteractionCreate, sub *discordgo.ApplicationCommandInteractionDataOption) {
	team := stringOption(sub, "team")
	challenge := stringOption(sub, "challenge")
	instances, err := b.kimo.ListInstances(team, challenge)
	if err != nil {
		b.respond(s, i, "Error: "+err.Error())
		return
	}
	b.respondEmbed(s, i, formatInstancesListEmbed(instances))
}

func (b *Bot) handleStats(s *discordgo.Session, i *discordgo.InteractionCreate) {
	templates, err := b.kimo.ListTemplates()
	if err != nil {
		b.respond(s, i, "Error: "+err.Error())
		return
	}
	instances, err := b.kimo.ListInstances("", "")
	if err != nil {
		b.respond(s, i, "Error: "+err.Error())
		return
	}
	b.respondEmbed(s, i, formatStatsEmbed(templates, instances))
}

func (b *Bot) handleMonitorStart(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if b.monitor == nil {
		b.respond(s, i, "Event monitor is not configured on this deployment.")
		return
	}
	b.monitor.SetTarget(i.ChannelID, true)
	b.respond(s, i, "Started posting lifecycle events to this channel.")
}

func (b *Bot) handleMonitorStop(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if b.monitor == nil {
		b.respond(s, i, "Event monitor is not configured on this deployment.")
		return
	}
	b.monitor.SetTarget("", false)
	b.respond(s, i, "Stopped posting lifecycle events.")
}

func stringOption(sub *discordgo.ApplicationCommandInteractionDataOption, name string) string {
	for _, opt := range sub.Options {
		if opt.Name == name {
			return opt.StringValue()
		}
	}
	return ""
}

func (b *Bot) respond(s *discordgo.Session, i *discordgo.InteractionCreate, content string) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: content},
	})
}

func (b *Bot) respondEmbed(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{embed}},
	})
}
