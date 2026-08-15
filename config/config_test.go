package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/config"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	Address string        `env:"TEST_ADDRESS" envDefault:":50051"`
	Debug   bool          `env:"TEST_DEBUG"`
	Timeout time.Duration `env:"TEST_TIMEOUT" envDefault:"5s"`
	Tags    []string      `env:"TEST_TAGS" envSeparator:","`
}

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()

	for _, key := range []string{"TEST_ADDRESS", "TEST_DEBUG", "TEST_TIMEOUT", "TEST_TAGS"} {
		t.Cleanup(func() { _ = os.Unsetenv(key) })
	}

	path := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func TestLoadUsesDefaults(t *testing.T) {
	cfg, err := config.Load[testConfig](writeEnvFile(t, ""))
	require.NoError(t, err)

	require.Equal(t, ":50051", cfg.Address)
	require.Equal(t, 5*time.Second, cfg.Timeout)
	require.False(t, cfg.Debug)
}

func TestLoadReadsEnvironment(t *testing.T) {
	t.Setenv("TEST_ADDRESS", ":9000")
	t.Setenv("TEST_DEBUG", "true")
	t.Setenv("TEST_TIMEOUT", "1m")
	t.Setenv("TEST_TAGS", "a,b,c")

	cfg, err := config.Load[testConfig](writeEnvFile(t, ""))
	require.NoError(t, err)

	require.Equal(t, ":9000", cfg.Address)
	require.True(t, cfg.Debug)
	require.Equal(t, time.Minute, cfg.Timeout)
	require.Equal(t, []string{"a", "b", "c"}, cfg.Tags)
}

func TestLoadReadsEnvFile(t *testing.T) {
	path := writeEnvFile(t, "TEST_ADDRESS=:7000\nTEST_DEBUG=true\n")

	cfg, err := config.Load[testConfig](path)
	require.NoError(t, err)

	require.Equal(t, ":7000", cfg.Address)
	require.True(t, cfg.Debug)
}

func TestEnvironmentWinsOverFile(t *testing.T) {
	t.Setenv("TEST_ADDRESS", ":9000")

	path := writeEnvFile(t, "TEST_ADDRESS=:7000\n")

	cfg, err := config.Load[testConfig](path)
	require.NoError(t, err)

	require.Equal(t, ":9000", cfg.Address)
}

func TestMissingFileIsNotAnError(t *testing.T) {
	cfg, err := config.Load[testConfig](filepath.Join(t.TempDir(), "absent.env"))

	require.NoError(t, err)
	require.Equal(t, ":50051", cfg.Address)
}

func TestLoadReportsInvalidValue(t *testing.T) {
	t.Setenv("TEST_TIMEOUT", "not-a-duration")

	_, err := config.Load[testConfig](writeEnvFile(t, ""))

	require.ErrorContains(t, err, "Timeout")
	require.ErrorContains(t, err, "not-a-duration")
}

func TestLoadIntoKeepsPresetFields(t *testing.T) {
	cfg := testConfig{Tags: []string{"preset"}}

	require.NoError(t, config.LoadInto(&cfg, writeEnvFile(t, "")))

	require.Equal(t, []string{"preset"}, cfg.Tags)
	require.Equal(t, ":50051", cfg.Address)
}
