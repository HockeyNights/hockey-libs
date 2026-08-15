package grpcserver

import (
	"net"
)

func LoopbackTarget(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {

		return net.JoinHostPort("localhost", listen)
	}

	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "localhost"
	}

	return net.JoinHostPort(host, port)
}
