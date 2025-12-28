package grpc

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	pb "mock-go-server/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

type connTracker struct {
	conns map[string]net.Conn
	mu    sync.RWMutex
}

func newConnTracker() *connTracker {
	return &connTracker{
		conns: make(map[string]net.Conn),
	}
}

func (ct *connTracker) add(addr string, conn net.Conn) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.conns[addr] = conn
}

func (ct *connTracker) remove(addr string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	delete(ct.conns, addr)
}

func (ct *connTracker) get(addr string) (net.Conn, bool) {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	conn, ok := ct.conns[addr]
	return conn, ok
}

type trackedListener struct {
	net.Listener
	tracker *connTracker
}

func (tl *trackedListener) Accept() (net.Conn, error) {
	conn, err := tl.Listener.Accept()
	if err != nil {
		return nil, err
	}

	addr := conn.RemoteAddr().String()
	tl.tracker.add(addr, conn)

	return &trackedConn{
		Conn:    conn,
		addr:    addr,
		tracker: tl.tracker,
	}, nil
}

type trackedConn struct {
	net.Conn
	addr    string
	tracker *connTracker
}

func (tc *trackedConn) Close() error {
	tc.tracker.remove(tc.addr)
	return tc.Conn.Close()
}

type Server struct {
	pb.UnimplementedMockServiceServer
	ServiceName string
	Port        string
	grpcServer  *grpc.Server
	tracker     *connTracker
}

func NewServer(serviceName, port string) *Server {
	return &Server{
		ServiceName: serviceName,
		Port:        port,
		tracker:     newConnTracker(),
	}
}

func (s *Server) Start() error {
	lis, err := net.Listen("tcp", ":"+s.Port)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}

	trackedLis := &trackedListener{
		Listener: lis,
		tracker:  s.tracker,
	}

	s.grpcServer = grpc.NewServer(
		grpc.UnaryInterceptor(s.loggingInterceptor),
	)
	pb.RegisterMockServiceServer(s.grpcServer, s)
	reflection.Register(s.grpcServer)

	log.Printf("[%s] Starting gRPC server on port %s (reflection enabled)", s.ServiceName, s.Port)
	return s.grpcServer.Serve(trackedLis)
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

func (s *Server) Disconnect(ctx context.Context, req *pb.DisconnectRequest) (*pb.Empty, error) {
	ms := req.GetMilliseconds()
	if ms < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Invalid delay: must be >= 0")
	}

	log.Printf("[%s] Disconnecting client connection after %dms", s.ServiceName, ms)
	time.Sleep(time.Duration(ms) * time.Millisecond)

	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get peer info")
	}
	if conn, ok := p.Addr.(interface{ Close() error }); ok {
		conn.Close()
	}
	return nil, status.Errorf(codes.Unavailable, "connection closed by server")
}

func (s *Server) Reset(ctx context.Context, req *pb.ResetRequest) (*pb.Empty, error) {
	ms := req.GetMilliseconds()
	if ms < 0 {
		return nil, status.Errorf(codes.InvalidArgument, "Invalid delay: must be >= 0")
	}

	log.Printf("[%s] Sending TCP RST to client connection after %dms", s.ServiceName, ms)
	time.Sleep(time.Duration(ms) * time.Millisecond)

	p, ok := peer.FromContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Internal, "failed to get peer info")
	}

	addr := p.Addr.String()
	conn, ok := s.tracker.get(addr)
	if !ok {
		return nil, status.Errorf(codes.Internal, "connection not found")
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetLinger(0)
	}

	conn.Close()
	s.tracker.remove(addr)

	return nil, status.Errorf(codes.Aborted, "TCP RST sent")
}
