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
	"github.com/bwmarrin/discordgo"
)

// Config carries everything the bot needs to talk to Discord and KIMO.
type Config struct {
	Token      string
	KIMOUrl    string
	KIMOApiKey string
	AdminRole  string
	OrgRole    string
	// WebhookAddr is where the event monitor's HTTP server listens for
	// the generic backend's webhook fan-out, e.g. ":8090". Empty disables it.
	WebhookAddr string
}

type Bot struct {
	session *discordgo.Session
	kimo    *KIMOClient
	config  Config
	monitor *Monitor
}

func New(cfg Config) (*Bot, error) {
	session, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		return nil, err
	}
	b := &Bot{
		session: session,
		kimo:    NewKIMOClient(cfg.KIMOUrl, cfg.KIMOApiKey),
		config:  cfg,
	}
	b.monitor = &Monitor{session: session}
	b.registerHandlers()
	return b, nil
}

// Start opens the Discord session, registers slash commands with Discord,
// and — if configured — starts the webhook event monitor's HTTP server.
func (b *Bot) Start() error {
	if err := b.session.Open(); err != nil {
		return err
	}
	if err := b.registerCommands(); err != nil {
		return err
	}
	if b.config.WebhookAddr != "" {
		b.monitor.Start(b.config.WebhookAddr)
	}
	return nil
}

func (b *Bot) Stop() error {
	if b.monitor != nil {
		b.monitor.Stop()
	}
	return b.session.Close()
}
