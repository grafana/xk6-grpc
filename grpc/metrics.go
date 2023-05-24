package grpc

import "go.k6.io/k6/metrics"

// grpcMetrics contains the metrics for the grpc extension.
type grpcMetrics struct {
	GRPCStreams                 *metrics.Metric
	GRPCStreamsMessagesSent     *metrics.Metric
	GRPCStreamsMessagesReceived *metrics.Metric
}

// registerMetrics registers and returns the metrics in the provided registry
func registerMetrics(registry *metrics.Registry) (*grpcMetrics, error) {
	var err error
	m := &grpcMetrics{}

	if m.GRPCStreams, err = registry.NewMetric("grpc_streams", metrics.Counter); err != nil {
		return nil, err
	}

	if m.GRPCStreamsMessagesSent, err = registry.NewMetric("grpc_streams_msgs_sent", metrics.Counter); err != nil {
		return nil, err
	}

	if m.GRPCStreamsMessagesReceived, err = registry.NewMetric("grpc_streams_msgs_received", metrics.Counter); err != nil {
		return nil, err
	}

	return m, nil
}
