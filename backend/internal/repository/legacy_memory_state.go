package repository

import (
	"errors"
	"fmt"
	"os"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var ErrLegacyMemoryState = errors.New("legacy memory state requires offline import into the primary database")

// Earlier PG+memory builds wrote an independent SQLite state file. Do not
// silently abandon its tokens, tasks or quota snapshots when changing stores.
func rejectLegacyMemoryState(cfg *config.Config) error {
	if !cfg.Database.IsPostgres() || !cfg.Redis.IsMemory() {
		return nil
	}
	legacy := cfg.Redis.Memory
	if err := legacy.ResolvePersistencePath("."); err != nil {
		return err
	}
	for _, path := range []string{legacy.PersistencePath, legacy.PersistencePath + "-wal", legacy.PersistencePath + "-journal"} {
		info, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect legacy memory state %q: %w", path, err)
		}
		if !info.Mode().IsRegular() || info.Size() > 0 {
			return fmt.Errorf("%w: %q; keep the source and stop all old writers before migration", ErrLegacyMemoryState, path)
		}
	}
	return nil
}
