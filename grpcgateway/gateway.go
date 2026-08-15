package grpcgateway

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/HockeyNights/hockey-libs/requestid"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

type RegisterFunc func(ctx context.Context, mux *runtime.ServeMux, conn *grpc.ClientConn) error

type Upstream struct {
	Name     string
	Target   string
	Register []RegisterFunc
}

type Config struct {
	Address string `env:"HTTP_API_ADDRESS"`
	Enabled bool   `env:"HTTP_API_ENABLED" envDefault:"true"`

	AllowedOrigins []string `env:"HTTP_API_ALLOWED_ORIGINS" envSeparator:","`

	Logger *slog.Logger `env:"-"`

	DialTimeout time.Duration `env:"HTTP_API_DIAL_TIMEOUT" envDefault:"5s"`
}

func New(ctx context.Context, cfg Config, upstreams ...Upstream) (http.Handler, error) {
	cfg = cfg.withDefaults()

	if cfg.Address == "" {
		return nil, fmt.Errorf("grpcgateway: HTTP_API_ADDRESS is required when the gateway is enabled")
	}
	if len(upstreams) == 0 {
		return nil, fmt.Errorf("grpcgateway: at least one upstream is required")
	}

	mux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, marshaler()),
		runtime.WithIncomingHeaderMatcher(incomingHeaderMatcher),
		runtime.WithOutgoingHeaderMatcher(outgoingHeaderMatcher),
	)

	for _, upstream := range upstreams {
		if upstream.Target == "" {
			return nil, fmt.Errorf("grpcgateway: upstream %q has no target", upstream.Name)
		}

		conn, err := grpc.NewClient(
			dialTarget(upstream.Target),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			return nil, fmt.Errorf("grpcgateway: dial %s: %w", upstream.Target, err)
		}

		for _, register := range upstream.Register {
			if err := register(ctx, mux, conn); err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("grpcgateway: register %s: %w", upstream.Name, err)
			}
		}

		cfg.Logger.Info("gateway upstream registered", "upstream", upstream.Name, "target", upstream.Target)
	}

	var handler http.Handler = mux
	if len(cfg.AllowedOrigins) > 0 {
		handler = WithCORS(handler, cfg.AllowedOrigins)
	}

	cfg.Logger.Info("http gateway ready", "address", cfg.Address, "upstreams", len(upstreams))

	return handler, nil
}

func dialTarget(target string) string {
	if strings.HasPrefix(target, ":") {
		return "localhost" + target
	}
	return target
}

func marshaler() runtime.Marshaler {
	return &runtime.JSONPb{
		MarshalOptions: protojson.MarshalOptions{
			UseProtoNames:   false,
			EmitUnpopulated: true,
		},
		UnmarshalOptions: protojson.UnmarshalOptions{
			DiscardUnknown: false,
		},
	}
}

func incomingHeaderMatcher(key string) (string, bool) {
	switch strings.ToLower(key) {
	case "authorization", requestid.MetadataKey:
		return strings.ToLower(key), true
	case "user-agent":
		return "x-client-user-agent", true
	default:
		return runtime.DefaultHeaderMatcher(key)
	}
}

func outgoingHeaderMatcher(key string) (string, bool) {
	if strings.EqualFold(key, requestid.MetadataKey) {
		return requestid.MetadataKey, true
	}
	return runtime.DefaultHeaderMatcher(key)
}

func (c Config) withDefaults() Config {
	if c.DialTimeout == 0 {
		c.DialTimeout = 5 * time.Second
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}
