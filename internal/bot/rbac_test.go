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
)

func TestRBAC_IsOrganizerAcceptsOrgOrAdminRole(t *testing.T) {
	b := &Bot{config: Config{AdminRole: "admin-role-id", OrgRole: "org-role-id"}}

	assert.True(t, b.isOrganizer(&discordgo.Member{Roles: []string{"org-role-id"}}))
	assert.True(t, b.isOrganizer(&discordgo.Member{Roles: []string{"admin-role-id"}}))
	assert.False(t, b.isOrganizer(&discordgo.Member{Roles: []string{"player-role-id"}}))
}

func TestRBAC_IsAdminRequiresAdminRoleSpecifically(t *testing.T) {
	b := &Bot{config: Config{AdminRole: "admin-role-id", OrgRole: "org-role-id"}}

	assert.True(t, b.isAdmin(&discordgo.Member{Roles: []string{"admin-role-id"}}))
	assert.False(t, b.isAdmin(&discordgo.Member{Roles: []string{"org-role-id"}}))
}
