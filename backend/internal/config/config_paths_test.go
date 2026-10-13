package config

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigureConfigSourcePlatformDefaultsAndOverrides(t *testing.T) {
	defaultPaths := []string{".", "./config"}
	if runtime.GOOS != "windows" {
		defaultPaths = []string{"/app/data", ".", "./config", "/etc/sub2api"}
	}
	dataDir := t.TempDir()
	explicitFile := filepath.Join(t.TempDir(), "explicit.yaml")
	tests := []struct {
		name       string
		dataDir    string
		configFile string
		wantFile   string
		wantPaths  []string
	}{
		{name: "defaults", wantPaths: defaultPaths},
		{
			name:      "explicit data directory first",
			dataDir:   " " + dataDir + " ",
			wantPaths: append([]string{dataDir}, defaultPaths...),
		},
		{
			name:       "explicit file overrides search paths",
			dataDir:    dataDir,
			configFile: " " + explicitFile + " ",
			wantFile:   explicitFile,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATA_DIR", tt.dataDir)
			t.Setenv("CONFIG_FILE", tt.configFile)
			var file string
			var paths []string
			configureConfigSource(
				func(value string) { file = value },
				func(value string) { paths = append(paths, value) },
			)
			require.Equal(t, tt.wantFile, file)
			require.Equal(t, tt.wantPaths, paths)
		})
	}
}
