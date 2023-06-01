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

func TestStream_InvalidHeader(t *testing.T) {
	t.Parallel()

	ts := newTestState(t)

	replace := func(code string) (goja.Value, error) {
		return ts.VU.Runtime().RunString(ts.httpBin.Replacer.Replace(code))
	}

	initString := codeBlock{
		code: `
		var client = new grpc.Client();
		client.load([], "../vendor/go.k6.io/k6/lib/testutils/httpmultibin/grpc_testing/test.proto");`,
	}

	val, err := replace(initString.code)
	assertResponse(t, initString, err, val, ts)

	ts.ToVUContext()

	_, err = replace(`
	client.connect("GRPCBIN_ADDR");
	new grpc.Stream(client, "foo/bar")`)

	assert.Error(t, err)
	assert.ErrorContains(t, err, `method "/foo/bar" not found in file descriptors`)
}

func TestStream_RequestHeaders(t *testing.T) {
	t.Parallel()

	ts := newTestState(t)

	var registeredMetadata metadata.MD
	stub := &FeatureExplorerStub{}
	stub.listFeatures = func(rect *grpcservice.Rectangle, stream grpcservice.FeatureExplorer_ListFeaturesServer) error {
		// collect metadata from the stream context
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

func TestStream_ErrorHandling(t *testing.T) {
	t.Parallel()

	ts := newTestState(t)

	stub := &FeatureExplorerStub{}

	savedFeatures := []*grpcservice.Feature{
		{
			Name: "foo",
			Location: &grpcservice.Point{
				Latitude:  1,
				Longitude: 2,
			},
		},
		{
			Name: "bar",
			Location: &grpcservice.Point{
				Latitude:  3,
				Longitude: 4,
			},
		},
	}

	stub.listFeatures = func(rect *grpcservice.Rectangle, stream grpcservice.FeatureExplorer_ListFeaturesServer) error {
		for _, feature := range savedFeatures {
			if err := stream.Send(feature); err != nil {
				return err
			}
		}

		return status.Error(codes.Internal, "lorem ipsum")
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
		let stream = new grpc.Stream(client, "main.FeatureExplorer/ListFeatures")
		stream.write({
			lo: {
			  latitude: 1,
			  longitude: 2,
			},
			hi: {
			  latitude: 1,
			  longitude: 2,
			},
		});
		stream.on('data', function (data) {
			call('Feature:' + data.name);
		})
		stream.on('error', function (e) {
			call('Code: ' + e.code + ' Message: ' + e.message);
		});
		`,
	}

	val, err := replace(initString.code)
	assertResponse(t, initString, err, val, ts)

	ts.ToVUContext()

	val, err = replace(vuString.code)

	ts.EventLoop.WaitOnRegistered()

	assertResponse(t, vuString, err, val, ts)

	assert.Equal(t, ts.callRecorder.Recorded(), []string{
		"Feature:foo",
		"Feature:bar",
		"Code: 13 Message: lorem ipsum",
	},
	)
}

// FeatureExplorerStub is a stub for FeatureExplorerServer
// it has ability to override methods
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
