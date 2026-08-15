package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

type Format string

const (
	FormatJSON Format = "json"
	FormatText Format = "text"
)

type Config struct {
	Level     string `env:"LOG_LEVEL" envDefault:"info"`
	Format    Format `env:"LOG_FORMAT" envDefault:"json"`
	AddSource bool   `env:"LOG_ADD_SOURCE"`
	Service   string `env:"SERVICE_NAME"`
	Version   string `env:"SERVICE_VERSION"`

	Output io.Writer `env:"-"`
}

func New(cfg Config) (*slog.Logger, error) {
	level, err := ParseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}

	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}

	options := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler
	switch Format(strings.ToLower(string(cfg.Format))) {
	case "", FormatJSON:
		handler = slog.NewJSONHandler(output, options)
	case FormatText:
		handler = slog.NewTextHandler(output, options)
	default:
		return nil, fmt.Errorf("logger: unknown format %q", cfg.Format)
	}

	handler = &contextHandler{Handler: handler}

	attrs := make([]slog.Attr, 0, 2)
	if cfg.Service != "" {
		attrs = append(attrs, slog.String("service", cfg.Service))
	}
	if cfg.Version != "" {
		attrs = append(attrs, slog.String("version", cfg.Version))
	}
	if len(attrs) > 0 {
		handler = handler.WithAttrs(attrs)
	}

	return slog.New(handler), nil
}

func MustNew(cfg Config) *slog.Logger {
	log, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func Discard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func ParseLevel(value string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("logger: unknown level %q", value)
	}
}

type loggerContextKey struct{}

func Into(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, log)
}

func From(ctx context.Context) *slog.Logger {
	if ctx != nil {
		if log, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok && log != nil {
			return log
		}
	}
	return slog.Default()
}

func With(ctx context.Context, args ...any) context.Context {
	return Into(ctx, From(ctx).With(args...))
}
