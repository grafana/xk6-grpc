// Package testutils provides utilities for testing grpc-go.
package testutils

import (
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/grafana/xk6-grpc/grpc/testutils/grpcservice"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// GRPC .
type GRPC struct {
	Addr       string
	ServerGRPC *grpc.Server
	Replacer   *strings.Replacer
}

// NewGRPC .
func NewGRPC(t testing.TB) *GRPC {
	grpcServer := grpc.NewServer()

	addr := getFreeBindAddr(t)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}

	features := grpcservice.LoadFeatures("")
	grpcservice.RegisterRouteGuideServer(grpcServer, grpcservice.NewRouteGuideServer(features...))
	grpcservice.RegisterFeatureExplorerServer(grpcServer, grpcservice.NewFeatureExplorerServer(features...))
	reflection.Register(grpcServer)

	go func() {
		_ = grpcServer.Serve(lis)
	}()

	t.Cleanup(func() {
		grpcServer.Stop()
	})

	return &GRPC{
		Addr:       addr,
		ServerGRPC: grpcServer,
		Replacer: strings.NewReplacer(
			"GRPCBIN_ADDR", addr,
		),
	}
}

var portRangeStart uint64 = 6565 //nolint:gochecknoglobals

func getFreeBindAddr(tb testing.TB) string {
	for i := 0; i < 100; i++ {
		port := atomic.AddUint64(&portRangeStart, 1)
		addr := net.JoinHostPort("localhost", strconv.FormatUint(port, 10))

		listener, err := net.Listen("tcp", addr)
		if err != nil {
			continue // port was busy for some reason
		}
		defer func() {
			assert.NoError(tb, listener.Close())
		}()
		return addr
	}

	tb.Fatal("could not get a free port")
	return ""
}
