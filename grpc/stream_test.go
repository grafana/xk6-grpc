package grpc

import (
	"context"
	"testing"

	"github.com/dop251/goja"
	"github.com/grafana/xk6-grpc/grpc/testutils/grpcservice"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type FeatureExplorerStub struct {
	grpcservice.UnimplementedFeatureExplorerServer

	getFeature   func(ctx context.Context, point *grpcservice.Point) (*grpcservice.Feature, error)
	listFeatures func(rect *grpcservice.Rectangle, stream grpcservice.FeatureExplorer_ListFeaturesServer) error
}

func (s *FeatureExplorerStub) GetFeature(ctx context.Context, point *grpcservice.Point) (*grpcservice.Feature, error) {
	if s.getFeature != nil {
		return s.getFeature(ctx, point)
	}

	return nil, status.Errorf(codes.Unimplemented, "method GetFeature not implemented")
}

func (s *FeatureExplorerStub) ListFeatures(rect *grpcservice.Rectangle, stream grpcservice.FeatureExplorer_ListFeaturesServer) error {
	if s.listFeatures != nil {
		return s.listFeatures(rect, stream)
	}

	return status.Errorf(codes.Unimplemented, "method ListFeatures not implemented")
}

func TestStream(t *testing.T) {
	t.Parallel()

	tests := []testcase{
		{
			name: "MethodNotFound",
			initString: codeBlock{
				code: `
				var client = new grpc.Client();
				client.load([], "../vendor/google.golang.org/grpc/test/grpc_testing/test.proto");`,
			},
			vuString: codeBlock{
				code: `
				client.connect("GRPCBIN_ADDR");
				new grpc.Stream(client, "foo/bar")`,
				err: `method "/foo/bar" not found in file descriptors`,
			},
		},
		{
			name: "InvalidParam",
			initString: codeBlock{code: `
				var client = new grpc.Client();
				client.load([], "../vendor/google.golang.org/grpc/test/grpc_testing/test.proto");`},
			vuString: codeBlock{
				code: `
				client.connect("GRPCBIN_ADDR");
				new grpc.Stream(client, "grpc.testing.TestService/EmptyCall", { void: true })`,
				err: `unknown param: "void"`,
			},
		},
		{
			name: "InvalidTimeoutType",
			initString: codeBlock{code: `
				var client = new grpc.Client();
				client.load([], "../vendor/google.golang.org/grpc/test/grpc_testing/test.proto");`},
			vuString: codeBlock{
				code: `
				client.connect("GRPCBIN_ADDR");
				new grpc.Stream(client, "grpc.testing.TestService/EmptyCall", { timeout: true })`,
				err: "invalid timeout value: unable to use type bool as a duration value",
			},
		},
		{
			name: "InvalidTimeout",
			initString: codeBlock{code: `
				var client = new grpc.Client();
				client.load([], "../vendor/google.golang.org/grpc/test/grpc_testing/test.proto");`},
			vuString: codeBlock{
				code: `
				client.connect("GRPCBIN_ADDR");
				new grpc.Stream(client, "grpc.testing.TestService/EmptyCall", { timeout: "please" })`,
				err: "invalid duration",
			},
		},
		{
			name: "StringTimeout",
			initString: codeBlock{code: `
				var client = new grpc.Client();
				client.load([], "../vendor/google.golang.org/grpc/test/grpc_testing/test.proto");`},
			vuString: codeBlock{
				code: `
				client.connect("GRPCBIN_ADDR");
				new grpc.Stream(client, "grpc.testing.TestService/EmptyCall", { timeout: "1h42m" })`,
			},
		},
		{
			name: "FloatTimeout",
			initString: codeBlock{code: `
				var client = new grpc.Client();
				client.load([], "../vendor/google.golang.org/grpc/test/grpc_testing/test.proto");`},
			vuString: codeBlock{
				code: `
				client.connect("GRPCBIN_ADDR");
				new grpc.Stream(client, "grpc.testing.TestService/EmptyCall", { timeout: 400.50 })`,
			},
		},
		{
			name: "IntegerTimeout",
			initString: codeBlock{
				code: `
				var client = new grpc.Client();
				client.load([], "../vendor/google.golang.org/grpc/test/grpc_testing/test.proto");`,
			},
			vuString: codeBlock{
				code: `
				client.connect("GRPCBIN_ADDR");
				new grpc.Stream(client, "grpc.testing.TestService/EmptyCall", { timeout: 2000 })`,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ts := newTestState(t)

			// setup necessary environment if needed by a test
			if tt.setup != nil {
				tt.setup(ts.httpBin)
			}

			replace := func(code string) (goja.Value, error) {
				return ts.VU.Runtime().RunString(ts.httpBin.Replacer.Replace(code))
			}

			val, err := replace(tt.initString.code)
			assertResponse(t, tt.initString, err, val, ts)

			ts.ToVUContext()

			val, err = replace(tt.vuString.code)
			assertResponse(t, tt.vuString, err, val, ts)
		})
	}
}

func TestStreamHeaders(t *testing.T) {
	t.Parallel()

	ts := newTestState(t)

	var registeredMetadata metadata.MD
	stub := &FeatureExplorerStub{}
	stub.listFeatures = func(rect *grpcservice.Rectangle, stream grpcservice.FeatureExplorer_ListFeaturesServer) error {
		md, ok := metadata.FromIncomingContext(stream.Context())
		if ok {
			registeredMetadata = md
		}

		return nil
	}

	grpcservice.RegisterFeatureExplorerServer(ts.httpBin.ServerGRPC, stub)

	replace := func(code string) (goja.Value, error) {
		return ts.VU.Runtime().RunString(ts.httpBin.Replacer.Replace(code))
	}

	initString := codeBlock{
		code: `
		var client = new grpc.Client();
		client.load([], "../grpc/testutils/grpcservice/route_guide.proto");`,
	}
	vuString := codeBlock{
		code: `
		client.connect("GRPCBIN_ADDR");
		let stream = new grpc.Stream(client, "main.FeatureExplorer/ListFeatures", { metadata: { "X-Load-Tester": "k6" } })
		stream.write({
			lo: {
			  latitude: 400000000,
			  longitude: -750000000,
			},
			hi: {
			  latitude: 420000000,
			  longitude: -730000000,
			},
		});
		`,
	}

	val, err := replace(initString.code)
	assertResponse(t, initString, err, val, ts)

	ts.ToVUContext()

	val, err = replace(vuString.code)

	ts.EventLoop.WaitOnRegistered()

	assertResponse(t, vuString, err, val, ts)

	// Check that the metadata was registered
	assert.Len(t, registeredMetadata["x-load-tester"], 1)
	assert.Equal(t, registeredMetadata["x-load-tester"][0], "k6")
}
