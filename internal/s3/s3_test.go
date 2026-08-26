package s3

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadDefaultAWSConfigUsesFallbackRegion(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config")
	credentialsPath := filepath.Join(dir, "credentials")

	require.NoError(t, os.WriteFile(configPath, nil, 0o600))
	require.NoError(t, os.WriteFile(credentialsPath, nil, 0o600))

	t.Setenv("AWS_CONFIG_FILE", configPath)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsPath)
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")

	cfg, err := loadDefaultAWSConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, defaultAWSRegion, cfg.Region)
}

func TestLoadDefaultAWSConfigPreservesConfiguredRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "eu-west-1")

	cfg, err := loadDefaultAWSConfig(context.Background())
	require.NoError(t, err)
	require.Equal(t, "eu-west-1", cfg.Region)
}
