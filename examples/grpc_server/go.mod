module github.com/grafana/xk6-grpc-example

go 1.19

replace github.com/grafana/xk6-grpc => ../../

require (
	github.com/grafana/xk6-grpc v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.53.0
	google.golang.org/protobuf v1.28.2-0.20230222093303-bc1253ad3743
)

require (
	github.com/golang/protobuf v1.5.3 // indirect
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sys v0.6.0 // indirect
	golang.org/x/text v0.8.0 // indirect
	google.golang.org/genproto v0.0.0-20230110181048-76db0878b65f // indirect
)
