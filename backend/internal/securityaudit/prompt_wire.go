package securityaudit

import (
	"github.com/Wei-Shaw/sub2api/internal/repository"
)

// ProvidePayloadStore selects the payload store that matches the configured
// cache backend. Memory deployments persist payloads in SQLite; Redis
// deployments keep the existing Redis-backed behavior.
func ProvidePayloadStore(resources *repository.BackendResources) PayloadStore {
	if resources == nil {
		return NewRedisPayloadStore(nil)
	}
	if resources.MemoryCacheEnabled() {
		return NewSQLitePayloadStore(resources.State.DB)
	}
	return NewRedisPayloadStore(resources.Redis)
}
