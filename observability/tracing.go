package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

type Config struct {
	Enabled bool `env:"OTEL_ENABLED"`

	ServiceName    string `env:"SERVICE_NAME"`
	ServiceVersion string `env:"SERVICE_VERSION"`
	Environment    string `env:"ENVIRONMENT" envDefault:"development"`

	Endpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT" envDefault:"localhost:4317"`
	Insecure bool   `env:"OTEL_EXPORTER_OTLP_INSECURE" envDefault:"true"`

	SampleRatio float64 `env:"OTEL_TRACES_SAMPLER_ARG" envDefault:"1.0"`

	ExportTimeout   time.Duration `env:"OTEL_EXPORT_TIMEOUT" envDefault:"10s"`
	ShutdownTimeout time.Duration `env:"OTEL_SHUTDOWN_TIMEOUT" envDefault:"5s"`
}

type Shutdown func(context.Context) error

func Init(ctx context.Context, cfg Config) (Shutdown, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !cfg.Enabled {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	cfg = cfg.withDefaults()

	options := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(cfg.Endpoint),
		otlptracegrpc.WithTimeout(cfg.ExportTimeout),
	}
	if cfg.Insecure {
		options = append(options, otlptracegrpc.WithInsecure())
	}

	exporter, err := otlptracegrpc.New(ctx, options...)
	if err != nil {
		return nil, fmt.Errorf("observability: create otlp exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("observability: build resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	)

	otel.SetTracerProvider(provider)

	shutdown := func(ctx context.Context) error {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.ShutdownTimeout)
		defer cancel()

		return errors.Join(
			provider.Shutdown(shutdownCtx),
			exporter.Shutdown(shutdownCtx),
		)
	}

	return shutdown, nil
}

func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

func (c Config) withDefaults() Config {
	if c.Endpoint == "" {
		c.Endpoint = "localhost:4317"
	}
	if c.Environment == "" {
		c.Environment = "development"
	}
	if c.ExportTimeout == 0 {
		c.ExportTimeout = 10 * time.Second
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 5 * time.Second
	}
	if c.SampleRatio <= 0 || c.SampleRatio > 1 {
		c.SampleRatio = 1
	}
	return c
}
