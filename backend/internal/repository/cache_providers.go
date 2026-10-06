package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func ProvideBillingCache(r *BackendResources) service.BillingCache {
	if r.memory != nil {
		return NewMemoryBillingCache(r.memory, r.State.DB)
	}
	return NewBillingCache(r.Redis)
}

func ProvideAPIKeyCache(r *BackendResources) service.APIKeyCache {
	if r.memory != nil {
		return NewMemoryAPIKeyCache(r.memory)
	}
	return NewAPIKeyCache(r.Redis)
}

func ProvideTempUnschedCache(r *BackendResources) service.TempUnschedCache {
	if r.memory != nil {
		return NewMemoryTempUnschedCache(r.memory)
	}
	return NewTempUnschedCache(r.Redis)
}

func ProvideTimeoutCounterCache(r *BackendResources) service.TimeoutCounterCache {
	if r.memory != nil {
		return NewMemoryTimeoutCounterCache(r.memory)
	}
	return NewTimeoutCounterCache(r.Redis)
}

func ProvideOpenAI403CounterCache(r *BackendResources) service.OpenAI403CounterCache {
	if r.memory != nil {
		return NewMemoryOpenAI403CounterCache(r.memory)
	}
	return NewOpenAI403CounterCache(r.Redis)
}

func ProvideInternal500CounterCache(r *BackendResources) service.Internal500CounterCache {
	if r.memory != nil {
		return NewMemoryInternal500CounterCache(r.memory)
	}
	return NewInternal500CounterCache(r.Redis)
}

func ProvideRPMCache(r *BackendResources) service.RPMCache {
	if r.memory != nil {
		return NewMemoryRPMCache(r.memory)
	}
	return NewRPMCache(r.Redis)
}

func ProvideUserRPMCache(r *BackendResources) service.UserRPMCache {
	if r.memory != nil {
		return NewMemoryUserRPMCache(r.memory)
	}
	return NewUserRPMCache(r.Redis)
}

func ProvideUserMsgQueueCache(r *BackendResources) service.UserMsgQueueCache {
	if r.memory != nil {
		return NewMemoryUserMsgQueueCache(r.memory)
	}
	return NewUserMsgQueueCache(r.Redis)
}

func ProvideEmailCache(r *BackendResources) service.EmailCache {
	if r.memory != nil {
		return NewMemoryEmailCache(r.memory)
	}
	return NewEmailCache(r.Redis)
}

func ProvideIdentityCache(r *BackendResources) service.IdentityCache {
	if r.memory != nil {
		return NewMemoryIdentityCache(r.memory)
	}
	return NewIdentityCache(r.Redis)
}

func ProvideRedeemCache(r *BackendResources) service.RedeemCache {
	if r.memory != nil {
		return NewMemoryRedeemCache(r.memory)
	}
	return NewRedeemCache(r.Redis)
}

func ProvideUpdateCache(r *BackendResources) service.UpdateCache {
	if r.memory != nil {
		return NewMemoryUpdateCache(r.memory)
	}
	return NewUpdateCache(r.Redis)
}

func ProvideGeminiTokenCache(r *BackendResources) service.GeminiTokenCache {
	if r.memory != nil {
		return NewMemoryGeminiTokenCache(r.memory)
	}
	return NewGeminiTokenCache(r.Redis)
}

func ProvideImageTaskStore(r *BackendResources) service.ImageTaskStore {
	if r.memory != nil {
		return NewMemoryImageTaskStore(r.memory, r.State.DB)
	}
	return NewImageTaskStore(r.Redis)
}

func ProvideLeaderLockCache(r *BackendResources) service.LeaderLockCache {
	if r.memory != nil {
		return NewMemoryLeaderLockCache(r.memory)
	}
	return NewLeaderLockCache(r.Redis)
}

func ProvideProxyLatencyCache(r *BackendResources) service.ProxyLatencyCache {
	if r.memory != nil {
		return NewMemoryProxyLatencyCache(r.memory)
	}
	return NewProxyLatencyCache(r.Redis)
}

func ProvideTotpCache(r *BackendResources) service.TotpCache {
	if r.memory != nil {
		return NewMemoryTotpCache(r.memory)
	}
	return NewTotpCache(r.Redis)
}

func ProvideRefreshTokenCache(r *BackendResources) service.RefreshTokenCache {
	if r.memory != nil {
		return NewMemoryRefreshTokenCache(r.memory, r.State.DB)
	}
	return NewRefreshTokenCache(r.Redis)
}

func ProvideErrorPassthroughCache(r *BackendResources) service.ErrorPassthroughCache {
	if r.memory != nil {
		return NewMemoryErrorPassthroughCache(r.memory)
	}
	return NewErrorPassthroughCache(r.Redis)
}

func ProvideTLSFingerprintProfileCache(r *BackendResources) service.TLSFingerprintProfileCache {
	if r.memory != nil {
		return NewMemoryTLSFingerprintProfileCache(r.memory)
	}
	return NewTLSFingerprintProfileCache(r.Redis)
}

func ProvideChannelCache(r *BackendResources) service.ChannelCachePubSub {
	if r.memory != nil {
		return NewMemoryChannelCache(r.memory)
	}
	return NewChannelCache(r.Redis)
}

func ProvideContentModerationHashCache(r *BackendResources) service.ContentModerationHashCache {
	if r.memory != nil {
		return NewMemoryContentModerationHashCache(r.memory)
	}
	return NewContentModerationHashCache(r.Redis)
}

func ProvidePluginKVStore(r *BackendResources) service.PluginKVStore {
	if r.memory != nil {
		return NewMemoryPluginKVStore(r.memory, r.State.DB)
	}
	return NewPluginKVStore(r.Redis)
}

func ProvidePasskeySessionStore(r *BackendResources) service.PasskeySessionStore {
	if r.memory != nil {
		return NewMemoryPasskeySessionStore(r.memory, r.State.DB)
	}
	return NewPasskeySessionStore(r.Redis)
}

func ProvideGatewayCache(r *BackendResources) service.GatewayCache {
	if r.memory != nil {
		return NewMemoryGatewayCache(r.memory, r.State.DB)
	}
	return NewGatewayCache(r.Redis)
}

func ProvideDashboardCache(r *BackendResources, cfg *config.Config) service.DashboardStatsCache {
	if r.memory != nil {
		return NewMemoryDashboardCache(r.memory, cfg)
	}
	return NewDashboardCache(r.Redis, cfg)
}

func ProvideBatchImageQueue(r *BackendResources, cfg *config.Config) service.BatchImageQueue {
	if r.memory != nil {
		return NewMemoryBatchImageQueue(r.memory, r.State.DB)
	}
	return NewBatchImageQueue(r.Redis, cfg)
}

func ProvideBatchImageDownloadLimiter(r *BackendResources, cfg *config.Config) service.BatchImageDownloadLimiter {
	if r.memory != nil {
		return NewMemoryBatchImageDownloadLimiter(r.memory, cfg)
	}
	return NewBatchImageDownloadLimiter(r.Redis, cfg)
}
