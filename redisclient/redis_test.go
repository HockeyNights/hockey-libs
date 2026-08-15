package redisclient_test

import (
	"testing"
	"time"

	"github.com/HockeyNights/hockey-libs/redisclient"
	"github.com/stretchr/testify/require"
)

func TestOptionsFromFields(t *testing.T) {
	options, err := redisclient.Config{
		Host:     "cache",
		Port:     6380,
		Password: "secret",
		DB:       3,
	}.Options()
	require.NoError(t, err)

	require.Equal(t, "cache:6380", options.Addr)
	require.Equal(t, "secret", options.Password)
	require.Equal(t, 3, options.DB)
}

func TestOptionsFromURL(t *testing.T) {
	options, err := redisclient.Config{
		URL: "redis://user:secret@cache:6380/2",
	}.Options()
	require.NoError(t, err)

	require.Equal(t, "cache:6380", options.Addr)
	require.Equal(t, "user", options.Username)
	require.Equal(t, 2, options.DB)
}

func TestOptionsApplyPoolSettingsToURL(t *testing.T) {
	options, err := redisclient.Config{
		URL:         "redis://cache:6379/0",
		PoolSize:    42,
		ReadTimeout: 7 * time.Second,
	}.Options()
	require.NoError(t, err)

	require.Equal(t, 42, options.PoolSize)
	require.Equal(t, 7*time.Second, options.ReadTimeout)
}

func TestOptionsFillsDefaults(t *testing.T) {
	options, err := redisclient.Config{}.Options()
	require.NoError(t, err)

	require.Equal(t, "localhost:6379", options.Addr)
	require.Equal(t, 10, options.PoolSize)
	require.Equal(t, 5*time.Second, options.DialTimeout)
}

func TestOptionsRejectsBadURL(t *testing.T) {
	_, err := redisclient.Config{URL: "://nope"}.Options()

	require.ErrorContains(t, err, "parse REDIS_URL")
}
