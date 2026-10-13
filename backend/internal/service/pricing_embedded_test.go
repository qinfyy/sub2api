package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPricingEmbeddedFallbackOffline(t *testing.T) {
	t.Chdir(t.TempDir())
	cfg := config.NewDefaultConfig()
	cfg.Pricing.RemoteURL = ""
	cfg.Pricing.HashURL = ""
	svc := NewPricingService(cfg, nil)
	require.NoError(t, svc.Initialize())
	defer svc.Stop()
	require.Greater(t, len(svc.pricingData), 100)
	require.FileExists(t, filepath.Join("data", "model_pricing.json"))
}

func TestPricingExplicitFallbackNeverReplaced(t *testing.T) {
	dir := t.TempDir()
	cfg := config.NewDefaultConfig()
	cfg.Pricing.DataDir = dir
	cfg.Pricing.FallbackFile = filepath.Join(dir, "custom.json")
	svc := NewPricingService(cfg, nil)
	require.Error(t, svc.useFallbackPricing())
	require.NoError(t, os.WriteFile(cfg.Pricing.FallbackFile, []byte("invalid"), 0600))
	require.Error(t, svc.useFallbackPricing())
	body := []byte(`{"custom":{"input_cost_per_token":0.1,"output_cost_per_token":0.2}}`)
	require.NoError(t, os.WriteFile(cfg.Pricing.FallbackFile, body, 0600))
	require.NoError(t, svc.useFallbackPricing())
	require.Len(t, svc.pricingData, 1)
	require.InDelta(t, 0.1, svc.pricingData["custom"].InputCostPerToken, 1e-10)
}
