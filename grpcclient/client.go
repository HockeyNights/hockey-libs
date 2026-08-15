package grpcclient

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

const defaultServiceConfig = `{
	"loadBalancingConfig": [{"round_robin": {}}],
	"methodConfig": [{
		"name": [{}],
		"retryPolicy": {
			"maxAttempts": 4,
			"initialBackoff": "0.1s",
			"maxBackoff": "2s",
			"backoffMultiplier": 2.0,
			"retryableStatusCodes": ["UNAVAILABLE", "RESOURCE_EXHAUSTED"]
		}
	}]
}`

type Config struct {
	Target string       `env:"GRPC_TARGET"`
	Logger *slog.Logger `env:"-"`

	TLS        *tls.Config `env:"-"`
	RequireTLS bool        `env:"GRPC_CLIENT_REQUIRE_TLS"`

	EnableTracing  bool `env:"GRPC_CLIENT_ENABLE_TRACING"`
	DisableRetries bool `env:"GRPC_CLIENT_DISABLE_RETRIES"`

	DefaultTimeout    time.Duration `env:"GRPC_CLIENT_TIMEOUT" envDefault:"5s"`
	ConnectTimeout    time.Duration `env:"GRPC_CLIENT_CONNECT_TIMEOUT" envDefault:"5s"`
	WaitForConnection bool          `env:"GRPC_CLIENT_WAIT_FOR_CONNECTION"`

	MaxRecvMsgSize int `env:"GRPC_CLIENT_MAX_RECV_MSG_SIZE" envDefault:"4194304"`
	MaxSendMsgSize int `env:"GRPC_CLIENT_MAX_SEND_MSG_SIZE" envDefault:"4194304"`

	ServiceConfig string `env:"-"`

	DialOptions        []grpc.DialOption              `env:"-"`
	UnaryInterceptors  []grpc.UnaryClientInterceptor  `env:"-"`
	StreamInterceptors []grpc.StreamClientInterceptor `env:"-"`
}

func New(ctx context.Context, cfg Config) (*grpc.ClientConn, error) {
	cfg = cfg.withDefaults()

	if cfg.Target == "" {
		return nil, errors.New("grpcclient: target is required")
	}

	transport, err := cfg.transportCredentials()
	if err != nil {
		return nil, err
	}

	options := []grpc.DialOption{
		transport,
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(cfg.MaxRecvMsgSize),
			grpc.MaxCallSendMsgSize(cfg.MaxSendMsgSize),
		),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}

	if !cfg.DisableRetries {
		serviceConfig := cfg.ServiceConfig
		if serviceConfig == "" {
			serviceConfig = defaultServiceConfig
		}
		options = append(options, grpc.WithDefaultServiceConfig(serviceConfig))
	}

	if cfg.EnableTracing {
		options = append(options, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	}

	unary := []grpc.UnaryClientInterceptor{
		RequestIDUnaryInterceptor(),
		ForwardTokenUnaryInterceptor(),
		TimeoutUnaryInterceptor(cfg.DefaultTimeout),
		LoggingUnaryInterceptor(cfg.Logger),
	}
	stream := []grpc.StreamClientInterceptor{
		RequestIDStreamInterceptor(),
		ForwardTokenStreamInterceptor(),
		LoggingStreamInterceptor(cfg.Logger),
	}

	unary = append(unary, cfg.UnaryInterceptors...)
	stream = append(stream, cfg.StreamInterceptors...)

	options = append(options,
		grpc.WithChainUnaryInterceptor(unary...),
		grpc.WithChainStreamInterceptor(stream...),
	)

	options = append(options, cfg.DialOptions...)

	conn, err := grpc.NewClient(cfg.Target, options...)
	if err != nil {
		return nil, err
	}

	if !cfg.WaitForConnection {
		cfg.Logger.Info("grpc client created", "target", cfg.Target)
		return conn, nil
	}

	if err := waitForConnection(ctx, conn, cfg.ConnectTimeout); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			cfg.Logger.Warn("grpc client close failed", "target", cfg.Target, "error", closeErr)
		}
		return nil, err
	}

	cfg.Logger.Info("grpc client connected", "target", cfg.Target)

	return conn, nil
}

func (c Config) transportCredentials() (grpc.DialOption, error) {
	if c.TLS != nil {
		return grpc.WithTransportCredentials(credentials.NewTLS(c.TLS)), nil
	}
	if c.RequireTLS {
		return nil, errors.New("grpcclient: RequireTLS is set but TLS config is missing")
	}

	return grpc.WithTransportCredentials(insecure.NewCredentials()), nil
}

func waitForConnection(ctx context.Context, conn *grpc.ClientConn, timeout time.Duration) error {
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn.Connect()

	for {
		state := conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return errors.New("grpcclient: connection is shut down")
		}
		if !conn.WaitForStateChange(connectCtx, state) {
			return connectCtx.Err()
		}
	}
}

func (c Config) withDefaults() Config {
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.DefaultTimeout == 0 {
		c.DefaultTimeout = 5 * time.Second
	}
	if c.MaxRecvMsgSize == 0 {
		c.MaxRecvMsgSize = 4 * 1024 * 1024
	}
	if c.MaxSendMsgSize == 0 {
		c.MaxSendMsgSize = 4 * 1024 * 1024
	}
	return c
}
