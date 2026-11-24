// mautrix-telegram - A Matrix-Telegram puppeting bridge.
// Copyright (C) 2026 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package connector

import (
	"testing"

	"maunium.net/go/mautrix/bridgev2/commands"
)

// TestCommandNamesUnique guards the digit handlers: every number from 1 to 99 is
// registered as a command (names or aliases), so a future command that reuses a
// number would silently shadow the panel shortcut.
func TestCommandNamesUnique(t *testing.T) {
	tc := &TelegramConnector{}
	seen := make(map[string]string)
	add := func(name, origin string) {
		if name == "" {
			return
		}
		if prev, ok := seen[name]; ok {
			t.Errorf("command %q is registered twice (%s and %s)", name, prev, origin)
			return
		}
		seen[name] = origin
	}
	for _, handler := range tc.buildCommands() {
		origin := handler.GetName()
		add(origin, origin)
		if aliased, ok := handler.(commands.AliasedCommandHandler); ok {
			for _, alias := range aliased.GetAliases() {
				add(alias, origin)
			}
		}
	}
	for _, digit := range []string{"1", "2", "50", "99"} {
		if _, ok := seen[digit]; !ok {
			t.Errorf("digit command %q is not registered", digit)
		}
	}
	if _, ok := seen["declined"]; !ok {
		t.Error("declined command is not registered")
	}
}
