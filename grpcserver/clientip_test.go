package grpcserver_test

import (
	"context"
	"net"
	"testing"

	"github.com/HockeyNights/hockey-libs/grpcserver"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
)

func ctxFrom(t *testing.T, peerAddr, forwarded string) context.Context {
	t.Helper()

	ctx := peer.NewContext(context.Background(), &peer.Peer{
		Addr: &net.TCPAddr{IP: net.ParseIP(peerAddr), Port: 51234},
	})

	if forwarded != "" {
		ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("x-forwarded-for", forwarded))
	}

	return ctx
}

func TestClientIPIgnoresForwardedHeaderFromUntrustedPeer(t *testing.T) {

	ctx := ctxFrom(t, "203.0.113.9", "1.2.3.4")

	require.Equal(t, "203.0.113.9", grpcserver.ClientIP(ctx, nil))
}

func TestClientIPTakesForwardedHeaderFromTrustedProxy(t *testing.T) {
	trusted, err := grpcserver.ParseTrustedProxies([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	ctx := ctxFrom(t, "10.0.0.7", "203.0.113.9")

	require.Equal(t, "203.0.113.9", grpcserver.ClientIP(ctx, trusted))
}

func TestClientIPSkipsTrustedHopsRightToLeft(t *testing.T) {

	trusted, err := grpcserver.ParseTrustedProxies([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	ctx := ctxFrom(t, "10.0.0.7", "198.51.100.4, 203.0.113.9, 10.0.0.3")

	require.Equal(t, "203.0.113.9", grpcserver.ClientIP(ctx, trusted))
}

func TestClientIPIgnoresSpoofedPrefixFromTrustedProxy(t *testing.T) {

	trusted, err := grpcserver.ParseTrustedProxies([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	ctx := ctxFrom(t, "10.0.0.7", "не-адрес, 203.0.113.9")

	require.Equal(t, "203.0.113.9", grpcserver.ClientIP(ctx, trusted))
}

func TestClientIPFallsBackToPeerWhenHeaderIsUnusable(t *testing.T) {
	trusted, err := grpcserver.ParseTrustedProxies([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	ctx := ctxFrom(t, "10.0.0.7", "мусор")

	require.Equal(t, "10.0.0.7", grpcserver.ClientIP(ctx, trusted))
}

func TestParseTrustedProxiesAcceptsBareAddress(t *testing.T) {
	trusted, err := grpcserver.ParseTrustedProxies([]string{"192.0.2.10", " ", "2001:db8::/32"})
	require.NoError(t, err)
	require.Len(t, trusted, 2)

	ctx := ctxFrom(t, "192.0.2.10", "203.0.113.9")
	require.Equal(t, "203.0.113.9", grpcserver.ClientIP(ctx, trusted))
}

func TestParseTrustedProxiesRejectsGarbage(t *testing.T) {
	_, err := grpcserver.ParseTrustedProxies([]string{"не подсеть"})
	require.Error(t, err)
}

func TestClientAddrReturnsNilWithoutPeer(t *testing.T) {
	require.Nil(t, grpcserver.ClientAddr(context.Background(), nil))
}
