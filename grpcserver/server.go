package grpcserver

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type RegisterFunc func(*grpc.Server)

type Config struct {
	Address string       `env:"GRPC_ADDRESS" envDefault:":50051"`
	Logger  *slog.Logger `env:"-"`

	EnableHealth     bool `env:"GRPC_ENABLE_HEALTH" envDefault:"true"`
	EnableReflection bool `env:"GRPC_ENABLE_REFLECTION"`
	EnableTracing    bool `env:"GRPC_ENABLE_TRACING"`

	GracefulStopTimeout time.Duration `env:"GRPC_GRACEFUL_STOP_TIMEOUT" envDefault:"10s"`
	DrainDelay          time.Duration `env:"GRPC_DRAIN_DELAY" envDefault:"3s"`

	MaxRecvMsgSize       int    `env:"GRPC_MAX_RECV_MSG_SIZE" envDefault:"4194304"`
	MaxSendMsgSize       int    `env:"GRPC_MAX_SEND_MSG_SIZE" envDefault:"4194304"`
	MaxConcurrentStreams uint32 `env:"GRPC_MAX_CONCURRENT_STREAMS" envDefault:"1000"`

	TLS *tls.Config `env:"-"`

	Registry *prometheus.Registry `env:"-"`

	LogSkipMethods []string `env:"-"`

	ServerOptions      []grpc.ServerOption            `env:"-"`
	UnaryInterceptors  []grpc.UnaryServerInterceptor  `env:"-"`
	StreamInterceptors []grpc.StreamServerInterceptor `env:"-"`
}

type Server struct {
	cfg       Config
	registers []RegisterFunc

	health *health.Server
}

func New(cfg Config, registers ...RegisterFunc) *Server {
	return &Server{
		cfg:       cfg.withDefaults(),
		registers: registers,
	}
}

func (s *Server) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", s.cfg.Address)
	if err != nil {
		return err
	}

	server := s.newGRPCServer()

	if s.cfg.EnableHealth {
		s.health = health.NewServer()
		s.health.SetServingStatus("", healthv1.HealthCheckResponse_SERVING)
		healthv1.RegisterHealthServer(server, s.health)
	}

	for _, register := range s.registers {
		register(server)
	}

	if s.cfg.EnableReflection {
		reflection.Register(server)
	}

	errCh := make(chan error, 1)
	go func() {
		s.cfg.Logger.Info("grpc server started",
			"address", listener.Addr().String(),
			"tls", s.cfg.TLS != nil,
			"reflection", s.cfg.EnableReflection,
		)
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		s.stop(server)
		return normalizeServeError(<-errCh)
	case err := <-errCh:
		return normalizeServeError(err)
	}
}

func (s *Server) Health() *health.Server {
	return s.health
}

func (s *Server) newGRPCServer() *grpc.Server {
	options := make([]grpc.ServerOption, 0, len(s.cfg.ServerOptions)+8)

	options = append(options,
		grpc.MaxRecvMsgSize(s.cfg.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(s.cfg.MaxSendMsgSize),
		grpc.MaxConcurrentStreams(s.cfg.MaxConcurrentStreams),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:                  30 * time.Second,
			Timeout:               10 * time.Second,
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 30 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)

	if s.cfg.TLS != nil {
		options = append(options, grpc.Creds(credentials.NewTLS(s.cfg.TLS)))
	}

	if s.cfg.EnableTracing {
		options = append(options, grpc.StatsHandler(otelgrpc.NewServerHandler()))
	}

	unary := []grpc.UnaryServerInterceptor{
		RecoveryUnaryInterceptor(s.cfg.Logger),
		RequestIDUnaryInterceptor(),
		LoggingUnaryInterceptor(s.cfg.Logger, s.cfg.LogSkipMethods...),
	}
	stream := []grpc.StreamServerInterceptor{
		RecoveryStreamInterceptor(s.cfg.Logger),
		RequestIDStreamInterceptor(),
		LoggingStreamInterceptor(s.cfg.Logger, s.cfg.LogSkipMethods...),
	}

	if s.cfg.Registry != nil {
		metrics := NewMetrics(s.cfg.Registry)
		unary = append(unary, metrics.UnaryInterceptor())
		stream = append(stream, metrics.StreamInterceptor())
	}

	unary = append(unary, ErrorMappingUnaryInterceptor())
	stream = append(stream, ErrorMappingStreamInterceptor())

	unary = append(unary, s.cfg.UnaryInterceptors...)
	stream = append(stream, s.cfg.StreamInterceptors...)

	options = append(options,
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(stream...),
	)

	options = append(options, s.cfg.ServerOptions...)

	return grpc.NewServer(options...)
}

func (s *Server) stop(server *grpc.Server) {
	if s.health != nil {
		s.health.SetServingStatus("", healthv1.HealthCheckResponse_NOT_SERVING)

		if s.cfg.DrainDelay > 0 {
			s.cfg.Logger.Info("grpc server draining", "delay", s.cfg.DrainDelay.String())
			time.Sleep(s.cfg.DrainDelay)
		}
	}

	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()

	timer := time.NewTimer(s.cfg.GracefulStopTimeout)
	defer timer.Stop()

	select {
	case <-stopped:
		s.cfg.Logger.Info("grpc server stopped")
	case <-timer.C:
		s.cfg.Logger.Warn("grpc server forced to stop", "timeout", s.cfg.GracefulStopTimeout.String())
		server.Stop()
		<-stopped
	}
}

func normalizeServeError(err error) error {
	if err == nil || errors.Is(err, grpc.ErrServerStopped) {
		return nil
	}
	return err
}

func (cfg Config) withDefaults() Config {
	if cfg.Address == "" {
		cfg.Address = ":50051"
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.GracefulStopTimeout == 0 {
		cfg.GracefulStopTimeout = 10 * time.Second
	}
	if cfg.MaxRecvMsgSize == 0 {
		cfg.MaxRecvMsgSize = 4 * 1024 * 1024
	}
	if cfg.MaxSendMsgSize == 0 {
		cfg.MaxSendMsgSize = 4 * 1024 * 1024
	}
	if cfg.MaxConcurrentStreams == 0 {
		cfg.MaxConcurrentStreams = 1000
	}

	cfg.LogSkipMethods = append([]string{
		healthv1.Health_Check_FullMethodName,
		healthv1.Health_Watch_FullMethodName,
		"/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
		"/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	}, cfg.LogSkipMethods...)

	return cfg
}
