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

package main

import (
	"log"
	"os"
	"os/signal"

	"github.com/hermannchristopher/kimo/internal/bot"
)

func main() {
	cfg := bot.Config{
		Token:       os.Getenv("DISCORD_TOKEN"),
		KIMOUrl:     os.Getenv("KIMO_API_URL"),
		KIMOApiKey:  os.Getenv("KIMO_API_KEY"),
		AdminRole:   os.Getenv("DISCORD_ADMIN_ROLE"),
		OrgRole:     os.Getenv("DISCORD_ORG_ROLE"),
		WebhookAddr: os.Getenv("KIMO_BOT_WEBHOOK_ADDR"),
	}

	b, err := bot.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	if err := b.Start(); err != nil {
		log.Fatal(err)
	}
	defer b.Stop()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
}
