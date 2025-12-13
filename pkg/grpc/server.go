package grpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"time"

	pb "mock-go-server/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type Server struct {
	pb.UnimplementedMockServiceServer
	ServiceName string
	Port        string
	grpcServer  *grpc.Server
}

func NewServer(serviceName, port string) *Server {
	return &Server{
		ServiceName: serviceName,
		Port:        port,
	}
}

func (s *Server) Start() error {
	lis, err := net.Listen("tcp", ":"+s.Port)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	s.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(s.loggingInterceptor),
	)
	pb.RegisterMockServiceServer(s.grpcServer, s)

	log.Printf("[%s] Starting gRPC server on port %s", s.ServiceName, s.Port)
	return s.grpcServer.Serve(lis)
}

func (s *Server) GracefulStop(ctx context.Context) error {
	log.Printf("[%s] Shutting down gRPC server...", s.ServiceName)

	done := make(chan struct{})
	go func() {
		s.grpcServer.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.grpcServer.Stop()
		return ctx.Err()
	}
}

func (s *Server) loggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()
	log.Printf("[%s] gRPC %s", s.ServiceName, info.FullMethod)
	resp, err := handler(ctx, req)
	log.Printf("[%s] gRPC %s completed in %v", s.ServiceName, info.FullMethod, time.Since(start))
	return resp, err
}

func (s *Server) Health(ctx context.Context, req *pb.Empty) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{
		Status:  "healthy",
		Service: s.ServiceName,
	}, nil
}

func (s *Server) Ready(ctx context.Context, req *pb.Empty) (*pb.ReadyResponse, error) {
	return &pb.ReadyResponse{
		Status:  "ready",
		Service: s.ServiceName,
	}, nil
}

func (s *Server) Status(ctx context.Context, req *pb.StatusRequest) (*pb.StatusResponse, error) {
	code := codes.Code(req.GetCode())

	if code != codes.OK {
		return nil, status.Errorf(code, "Simulated error with gRPC code %d (%s)", code, code.String())
	}

	return &pb.StatusResponse{
		StatusCode: int32(code),
		Service:    s.ServiceName,
		Message:    "OK",
	}, nil
}

func (s *Server) Delay(ctx context.Context, req *pb.DelayRequest) (*pb.DelayResponse, error) {
	ms := req.GetMilliseconds()
	if ms < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Invalid delay: must be >= 0")
	}

	time.Sleep(time.Duration(ms) * time.Millisecond)

	return &pb.DelayResponse{
		Service:   s.ServiceName,
		DelayedMs: ms,
		Message:   fmt.Sprintf("Response delayed by %dms", ms),
	}, nil
}

func (s *Server) Headers(ctx context.Context, req *pb.Empty) (*pb.HeadersResponse, error) {
	headers := make(map[string]string)

	if md, ok := metadata.FromIncomingContext(ctx); ok {
		for key, values := range md {
			if len(values) > 0 {
				headers[key] = values[0]
			}
		}
	}

	return &pb.HeadersResponse{
		Service: s.ServiceName,
		Headers: headers,
	}, nil
}

func (s *Server) Large(ctx context.Context, req *pb.LargeRequest) (*pb.LargeResponse, error) {
	size := int(req.GetSizeKb())
	if size <= 0 {
		size = 1000
	}
	if size > 100*1024 {
		size = 100 * 1024
	}

	data := make([]byte, size*1024)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}

	return &pb.LargeResponse{
		Data: data,
	}, nil
}

func (s *Server) Echo(ctx context.Context, req *pb.EchoRequest) (*pb.EchoResponse, error) {
	return &pb.EchoResponse{
		Data: req.GetData(),
	}, nil
}
