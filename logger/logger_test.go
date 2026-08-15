package logger_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/HockeyNights/hockey-libs/logger"
	"github.com/stretchr/testify/require"
)

func newTestLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()

	buf := &bytes.Buffer{}
	log, err := logger.New(logger.Config{
		Level:   "debug",
		Format:  logger.FormatJSON,
		Service: "auth",
		Version: "1.2.3",
		Output:  buf,
	})
	require.NoError(t, err)

	return log, buf
}

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()

	var record map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &record))

	return record
}

func TestNewAddsServiceAttributes(t *testing.T) {
	log, buf := newTestLogger(t)

	log.Info("started")

	record := decode(t, buf)
	require.Equal(t, "auth", record["service"])
	require.Equal(t, "1.2.3", record["version"])
	require.Equal(t, "started", record["msg"])
}

func TestNewRejectsUnknownLevel(t *testing.T) {
	_, err := logger.New(logger.Config{Level: "verbose"})
	require.Error(t, err)
}

func TestNewRejectsUnknownFormat(t *testing.T) {
	_, err := logger.New(logger.Config{Format: "xml"})
	require.Error(t, err)
}

func TestLevelFiltersRecords(t *testing.T) {
	buf := &bytes.Buffer{}
	log, err := logger.New(logger.Config{Level: "warn", Output: buf})
	require.NoError(t, err)

	log.Info("skipped")
	require.Empty(t, buf.String())

	log.Warn("kept")
	require.Contains(t, buf.String(), "kept")
}

func TestContextAttrsAreAdded(t *testing.T) {
	log, buf := newTestLogger(t)

	ctx := logger.Attrs(context.Background(), slog.String("request_id", "req-1"))
	ctx = logger.Attrs(ctx, slog.String("user_id", "user-1"))

	log.InfoContext(ctx, "handled")

	record := decode(t, buf)
	require.Equal(t, "req-1", record["request_id"])
	require.Equal(t, "user-1", record["user_id"])
}

func TestAttrsDoesNotLeakIntoSiblingContext(t *testing.T) {
	log, buf := newTestLogger(t)

	parent := logger.Attrs(context.Background(), slog.String("request_id", "req-1"))
	_ = logger.Attrs(parent, slog.String("user_id", "user-1"))

	log.InfoContext(parent, "handled")

	record := decode(t, buf)
	require.Equal(t, "req-1", record["request_id"])
	require.NotContains(t, record, "user_id")
}

func TestFromContextFallsBackToDefault(t *testing.T) {
	require.Equal(t, slog.Default(), logger.From(context.Background()))
	require.Equal(t, slog.Default(), logger.From(context.TODO()))
}

func TestIntoAndFrom(t *testing.T) {
	log, _ := newTestLogger(t)

	ctx := logger.Into(context.Background(), log)
	require.Equal(t, log, logger.From(ctx))
}

func TestWithAddsAttributesToContextLogger(t *testing.T) {
	log, buf := newTestLogger(t)

	ctx := logger.With(logger.Into(context.Background(), log), "component", "repository")
	logger.From(ctx).Info("query")

	record := decode(t, buf)
	require.Equal(t, "repository", record["component"])
}

func TestParseLevel(t *testing.T) {
	for value, expected := range map[string]slog.Level{
		"":        slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		" warn  ": slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	} {
		level, err := logger.ParseLevel(value)
		require.NoError(t, err, value)
		require.Equal(t, expected, level, value)
	}

	_, err := logger.ParseLevel("trace")
	require.Error(t, err)
}
