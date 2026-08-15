package grpcserver_test

import (
	"testing"

	"github.com/HockeyNights/hockey-libs/grpcserver"
	"github.com/stretchr/testify/require"
)

func TestLoopbackTarget(t *testing.T) {
	cases := map[string]string{

		":50051":        "localhost:50051",
		"0.0.0.0:50051": "localhost:50051",
		"[::]:50051":    "localhost:50051",

		"10.0.0.5:50051":  "10.0.0.5:50051",
		"localhost:50051": "localhost:50051",

		"50051": "localhost:50051",
	}

	for listen, want := range cases {
		require.Equal(t, want, grpcserver.LoopbackTarget(listen), "адрес %q", listen)
	}
}
