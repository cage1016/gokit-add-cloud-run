package transports

import (
	"context"

	kitjwt "github.com/go-kit/kit/auth/jwt"
	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/tracing/opentracing"
	"github.com/go-kit/kit/tracing/zipkin"
	grpctransport "github.com/go-kit/kit/transport/grpc"
	stdopentracing "github.com/opentracing/opentracing-go"
	stdzipkin "github.com/openzipkin/zipkin-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/cage1016/add/internal/app/add/endpoints"
	"github.com/cage1016/add/internal/app/add/service"
	"github.com/cage1016/add/internal/pkg/errors"
	pb "github.com/cage1016/add/pb/add"
)

type grpcServer struct {
	sum    grpctransport.Handler `json:""`
	concat grpctransport.Handler `json:""`
}

func (s *grpcServer) Sum(ctx context.Context, req *pb.SumRequest) (rep *pb.SumResponse, err error) {
	_, rp, err := s.sum.ServeGRPC(ctx, req)
	if err != nil {
		return nil, grpcEncodeError(errors.Cast(err))
	}
	rep = rp.(*pb.SumResponse)
	return rep, nil
}

func (s *grpcServer) Concat(ctx context.Context, req *pb.ConcatRequest) (rep *pb.ConcatResponse, err error) {
	_, rp, err := s.concat.ServeGRPC(ctx, req)
	if err != nil {
		return nil, grpcEncodeError(errors.Cast(err))
	}
	rep = rp.(*pb.ConcatResponse)
	return rep, nil
}

// MakeGRPCServer makes a set of endpoints available as a gRPC server.
func MakeGRPCServer(endpoints endpoints.Endpoints, otTracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) (req pb.AddServer) {
	options := []grpctransport.ServerOption{
		grpctransport.ServerErrorLogger(logger),
	}

	if zipkinTracer != nil {
		// Zipkin GRPC Server Trace can either be instantiated per gRPC method with a
		// provided operation name or a global tracing service can be instantiated
		// without an operation name and fed to each Go kit gRPC server as a
		// ServerOption.
		// In the latter case, the operation name will be the endpoint's grpc method
		// path if used in combination with the Go kit gRPC Interceptor.
		//
		// In this example, we demonstrate a global Zipkin tracing service with
		// Go kit gRPC Interceptor.
		options = append(options, zipkin.GRPCServerTrace(zipkinTracer))
	}

	return &grpcServer{
		sum: grpctransport.NewServer(
			endpoints.SumEndpoint,
			decodeGRPCSumRequest,
			encodeGRPCSumResponse,
			append(options, grpctransport.ServerBefore(opentracing.GRPCToContext(otTracer, "Sum", logger), kitjwt.GRPCToContext()))...,
		),

		concat: grpctransport.NewServer(
			endpoints.ConcatEndpoint,
			decodeGRPCConcatRequest,
			encodeGRPCConcatResponse,
			append(options, grpctransport.ServerBefore(opentracing.GRPCToContext(otTracer, "Concat", logger), kitjwt.GRPCToContext()))...,
		),
	}
}

// decodeGRPCSumRequest is a transport/grpc.DecodeRequestFunc that converts a
// gRPC request to a user-domain request. Primarily useful in a server.
func decodeGRPCSumRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.SumRequest)
	return endpoints.SumRequest{A: req.A, B: req.B}, nil
}

// encodeGRPCSumResponse is a transport/grpc.EncodeResponseFunc that converts a
// user-domain response to a gRPC reply. Primarily useful in a server.
func encodeGRPCSumResponse(_ context.Context, grpcReply interface{}) (res interface{}, err error) {
	reply := grpcReply.(endpoints.SumResponse)
	return &pb.SumResponse{Res: reply.Res}, grpcEncodeError(errors.Cast(reply.Err))
}

// decodeGRPCConcatRequest is a transport/grpc.DecodeRequestFunc that converts a
// gRPC request to a user-domain request. Primarily useful in a server.
func decodeGRPCConcatRequest(_ context.Context, grpcReq interface{}) (interface{}, error) {
	req := grpcReq.(*pb.ConcatRequest)
	return endpoints.ConcatRequest{A: req.A, B: req.B}, nil
}

// encodeGRPCConcatResponse is a transport/grpc.EncodeResponseFunc that converts a
// user-domain response to a gRPC reply. Primarily useful in a server.
func encodeGRPCConcatResponse(_ context.Context, grpcReply interface{}) (res interface{}, err error) {
	reply := grpcReply.(endpoints.ConcatResponse)
	return &pb.ConcatResponse{Res: reply.Res}, grpcEncodeError(errors.Cast(reply.Err))
}

// NewGRPCClient returns an AddService backed by a gRPC server at the other end
// of the conn. The caller is responsible for constructing the conn, and
// eventually closing the underlying transport. We bake-in certain middlewares,
// implementing the client library pattern.
func NewGRPCClient(conn *grpc.ClientConn, otTracer stdopentracing.Tracer, zipkinTracer *stdzipkin.Tracer, logger log.Logger) service.AddService { // Zipkin GRPC Client Trace can either be instantiated per gRPC method with a
	// global client middlewares
	options := []grpctransport.ClientOption{}

	if zipkinTracer != nil {
		// provided operation name or a global tracing client can be instantiated
		// without an operation name and fed to each Go kit client as ClientOption.
		// In the latter case, the operation name will be the endpoint's grpc method
		// path.
		//
		// In this example, we demonstrace a global tracing client.
		options = append(options, zipkin.GRPCClientTrace(zipkinTracer))
	}

	// The Sum endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var sumEndpoint endpoint.Endpoint
	{
		sumEndpoint = grpctransport.NewClient(
			conn,
			"pb.Add",
			"Sum",
			encodeGRPCSumRequest,
			decodeGRPCSumResponse,
			pb.SumResponse{},
			append(options, grpctransport.ClientBefore(opentracing.ContextToGRPC(otTracer, logger), kitjwt.ContextToGRPC()))...,
		).Endpoint()
		sumEndpoint = opentracing.TraceClient(otTracer, "Sum")(sumEndpoint)
	}

	// The Concat endpoint is the same thing, with slightly different
	// middlewares to demonstrate how to specialize per-endpoint.
	var concatEndpoint endpoint.Endpoint
	{
		concatEndpoint = grpctransport.NewClient(
			conn,
			"pb.Add",
			"Concat",
			encodeGRPCConcatRequest,
			decodeGRPCConcatResponse,
			pb.ConcatResponse{},
			append(options, grpctransport.ClientBefore(opentracing.ContextToGRPC(otTracer, logger), kitjwt.ContextToGRPC()))...,
		).Endpoint()
		concatEndpoint = opentracing.TraceClient(otTracer, "Concat")(concatEndpoint)
	}

	return endpoints.Endpoints{
		SumEndpoint:    sumEndpoint,
		ConcatEndpoint: concatEndpoint,
	}
}

// encodeGRPCSumRequest is a transport/grpc.EncodeRequestFunc that converts a
// user-domain Sum request to a gRPC Sum request. Primarily useful in a client.
func encodeGRPCSumRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(endpoints.SumRequest)
	return &pb.SumRequest{A: req.A, B: req.B}, nil
}

// decodeGRPCSumResponse is a transport/grpc.DecodeResponseFunc that converts a
// gRPC Sum reply to a user-domain Sum response. Primarily useful in a client.
func decodeGRPCSumResponse(_ context.Context, grpcReply interface{}) (interface{}, error) {
	reply := grpcReply.(*pb.SumResponse)
	return endpoints.SumResponse{Res: reply.Res}, nil
}

// encodeGRPCConcatRequest is a transport/grpc.EncodeRequestFunc that converts a
// user-domain Concat request to a gRPC Concat request. Primarily useful in a client.
func encodeGRPCConcatRequest(_ context.Context, request interface{}) (interface{}, error) {
	req := request.(endpoints.ConcatRequest)
	return &pb.ConcatRequest{A: req.A, B: req.B}, nil
}

// decodeGRPCConcatResponse is a transport/grpc.DecodeResponseFunc that converts a
// gRPC Concat reply to a user-domain Concat response. Primarily useful in a client.
func decodeGRPCConcatResponse(_ context.Context, grpcReply interface{}) (interface{}, error) {
	reply := grpcReply.(*pb.ConcatResponse)
	return endpoints.ConcatResponse{Res: reply.Res}, nil
}

func grpcEncodeError(err errors.Error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if ok {
		return status.Error(st.Code(), st.Message())
	}

	switch {
	// TODO write your own custom error check here
	case errors.Contains(err, kitjwt.ErrTokenContextMissing):
		return status.Error(codes.Unauthenticated, err.Error())
	default:
		return status.Error(codes.Internal, "internal server error")
	}
}
