// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"

	"github.com/tomlawesome/mikroview/internal/config"
	"github.com/tomlawesome/mikroview/internal/logging"
	"github.com/tomlawesome/mikroview/internal/settings"
)

// openStoreSettings decides how big this instance's event buffer is, and
// says so.
//
// Two figures can name that size: the one in config.yaml, and the one an
// admin set from the memory control inside the app (#796). The stored
// one wins. It is the more recent statement, and it was made with the
// actual trade-off on screen -- the hours currently held, what the new
// figure buys at today's rate, and what it would cost the host -- while
// the file's was written before the instance had ever seen traffic.
//
// The reverse rule would be worse in a specific way: an operator who
// lowered the buffer because the host could not carry the file's figure
// would find every restart putting it straight back, which is the
// failure they were trying to fix.
//
// Whichever wins, it is announced at startup naming both, because a
// setting that silently ignores the file it is written in is exactly the
// kind of thing an operator spends an afternoon on. To go back to the
// file's figure, delete the settings document (store.settingsStorePath)
// -- there is deliberately no "reset" endpoint, since moving the slider
// back is the same act and already exists.
//
// A settings document that exists but cannot be read is fatal, the same
// as every other store: see internal/persist.Open and #378 for why a
// store is never built around a backend whose load failed.
func openStoreSettings(ctx context.Context, persistence *storage, cfg config.Config) (config.ByteSize, int, *settings.Store) {
	log := logging.New("store")

	backend, err := persistence.backendFor(ctx, "settings", cfg.Store.SettingsStorePath)
	if err != nil {
		// Same treatment the sibling stores get for a backend that could
		// not be prepared: warn and carry on unpersisted, rather than
		// refusing to start over a setting that has a perfectly good
		// default.
		log.Warn(err.Error())
	}
	store, err := settings.OpenWithBackend(backend)
	mustOpenStore(log, err)

	maxMemory := cfg.Store.MaxMemory
	if stored, ok := store.MaxMemory(); ok {
		if config.ByteSize(stored) != cfg.Store.MaxMemory {
			log.Info(fmt.Sprintf(
				"event buffer: using the stored size %s, set from Settings, rather than config.yaml's store.maxMemory of %s -- delete %s to go back to the file's figure",
				config.ByteSize(stored), cfg.Store.MaxMemory, cfg.Store.SettingsStorePath))
		}
		maxMemory = config.ByteSize(stored)
	}

	capacity := config.Store{MaxMemory: maxMemory}.Capacity()
	log.Info(fmt.Sprintf(
		"event buffer: %s reserved for up to %d events (store.maxMemory) -- once traffic arrives, GET /api/stats reports how full it is and how far back it actually reaches",
		maxMemory, capacity))
	return maxMemory, capacity, store
}

// memoryBoundsBasis says, in a few words, what the ceiling was worked
// out from -- so the startup line names the reason as well as the
// number, and an operator who thinks the ceiling is wrong knows which
// figure to go and check.
func memoryBoundsBasis(b config.MemoryBounds) string {
	if b.HostTotal <= 0 {
		return b.Source
	}
	return fmt.Sprintf("%s of %s, from %s, kept back as headroom", b.HostTotal-b.Max, b.HostTotal, b.Source)
}
