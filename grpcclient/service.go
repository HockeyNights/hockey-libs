package grpcclient

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/HockeyNights/hockey-libs/token"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

const DefaultServiceTokenTTL = 5 * time.Minute

const serviceTokenRenewBefore = 30 * time.Second

type ServiceTokenSource struct {
	manager *token.Manager
	subject string
	ttl     time.Duration
	roles   []string

	mu      sync.Mutex
	cached  string
	expires time.Time

	now func() time.Time
}

func NewServiceTokenSource(manager *token.Manager, subject string, ttl time.Duration) *ServiceTokenSource {
	if ttl <= 0 {
		ttl = DefaultServiceTokenTTL
	}

	return &ServiceTokenSource{
		manager: manager,
		subject: subject,
		ttl:     ttl,
		roles:   []string{token.RoleService},
		now:     time.Now,
	}
}

func (s *ServiceTokenSource) Token() (string, error) {
	if s == nil || s.manager == nil {
		return "", fmt.Errorf("grpcclient: service token source is not configured")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if s.cached != "" && now.Before(s.expires.Add(-serviceTokenRenewBefore)) {
		return s.cached, nil
	}

	raw, claims, err := s.manager.IssueAccess(s.subject, token.IssueOptions{
		Roles: s.roles,
		TTL:   s.ttl,
	})
	if err != nil {
		return "", fmt.Errorf("grpcclient: issue service token: %w", err)
	}

	s.cached = raw
	s.expires = claims.ExpiresAt.Time

	return raw, nil
}

func ServiceTokenUnaryInterceptor(source *ServiceTokenSource) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		req, reply any,
		conn *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		opts ...grpc.CallOption,
	) error {
		raw, err := source.Token()
		if err != nil {
			return err
		}

		outgoing, ok := metadata.FromOutgoingContext(ctx)
		if ok {
			outgoing = outgoing.Copy()
		} else {
			outgoing = metadata.MD{}
		}
		outgoing.Set(authorizationKey, "Bearer "+raw)

		return invoker(metadata.NewOutgoingContext(ctx, outgoing), method, req, reply, conn, opts...)
	}
}
