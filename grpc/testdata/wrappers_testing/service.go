package wrappers_testing

import (
	context "context"

	wrappers "github.com/golang/protobuf/ptypes/wrappers"
	grpc "google.golang.org/grpc"
)

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative test.proto

// Register registers a test service that could be used for the testing gRPC wrappers
func Register(r grpc.ServiceRegistrar) *service {
	s := &service{}

	RegisterServiceServer(r, s)

	return s
}

type service struct {
	UnimplementedServiceServer

	TestStringImplementation  func(context.Context, *wrappers.StringValue) (*wrappers.StringValue, error)
	TestIntegerImplementation func(context.Context, *wrappers.Int64Value) (*wrappers.Int64Value, error)
	TestBooleanImplementation func(context.Context, *wrappers.BoolValue) (*wrappers.BoolValue, error)
	TestDoubleImplementation  func(context.Context, *wrappers.DoubleValue) (*wrappers.DoubleValue, error)
}

func (s *service) TestString(ctx context.Context, in *wrappers.StringValue) (*wrappers.StringValue, error) {
	if s.TestStringImplementation != nil {
		return s.TestStringImplementation(ctx, in)
	}

	return s.UnimplementedServiceServer.TestString(ctx, in)
}

func (s *service) TestInteger(ctx context.Context, in *wrappers.Int64Value) (*wrappers.Int64Value, error) {
	if s.TestIntegerImplementation != nil {
		return s.TestIntegerImplementation(ctx, in)
	}

	return s.UnimplementedServiceServer.TestInteger(ctx, in)
}

func (s *service) TestBoolean(ctx context.Context, in *wrappers.BoolValue) (*wrappers.BoolValue, error) {
	if s.TestBooleanImplementation != nil {
		return s.TestBooleanImplementation(ctx, in)
	}

	return s.UnimplementedServiceServer.TestBoolean(ctx, in)
}

func (s *service) TestDouble(ctx context.Context, in *wrappers.DoubleValue) (*wrappers.DoubleValue, error) {
	if s.TestBooleanImplementation != nil {
		return s.TestDoubleImplementation(ctx, in)
	}

	return s.UnimplementedServiceServer.TestDouble(ctx, in)
}
