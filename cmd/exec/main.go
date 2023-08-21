package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"knative.dev/pkg/environment"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
)

const (
	// connectionFailure indicates connection failed.
	connectionFailure = 2
	// repcFailure indicates rpc failed.
	rpcFailure = 3
	// unhealthy indicates rpc succeeded but indicates unhealthy service.
	unhealthy = 4

	timeout = 100 * time.Millisecond
)

var (
	port = flag.Int("port", 18000, "the port to serve on")

	fSet      = flag.NewFlagSet("probe", flag.ExitOnError)
	probeAddr = fSet.String("probe-addr", "", "run this binary as a health check against the given address")
)

type server struct {
	healthgrpc.UnimplementedHealthServer
}

// Check implements `service Health`.
func (s *server) Check(ctx context.Context, in *healthgrpc.HealthCheckRequest) (*healthgrpc.HealthCheckResponse, error) {
	log.Printf("%s: server.Check() called, returning: %v", time.Now().Format(time.RFC3339), healthgrpc.HealthCheckResponse_SERVING)
	return &healthgrpc.HealthCheckResponse{Status: healthgrpc.HealthCheckResponse_SERVING}, nil
}

func main() {
	env := new(environment.ClientConfig)
	env.InitFlags(fSet)
	_ = fSet.Parse(os.Args[1:])

	if *probeAddr != "" {
		os.Exit(check(*probeAddr))
	}

	opts := []logging.Option{
		logging.WithLogOnEvents(logging.StartCall, logging.PayloadReceived, logging.FinishCall, logging.PayloadSent),
	}

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			logging.UnaryServerInterceptor(logInterceptor(), opts...),
		),
		grpc.ChainStreamInterceptor(
			logging.StreamServerInterceptor(logInterceptor(), opts...),
		),
	)

	healthgrpc.RegisterHealthServer(s, &server{})

	log.Printf("%v: starting health check server", time.Now())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func logInterceptor() logging.Logger {
	return logging.LoggerFunc(func(_ context.Context, lvl logging.Level, msg string, fields ...any) {
		switch lvl {
		case logging.LevelDebug:
			msg = fmt.Sprintf("DEBUG :%v", msg)
		case logging.LevelInfo:
			msg = fmt.Sprintf("INFO :%v", msg)
		case logging.LevelWarn:
			msg = fmt.Sprintf("WARN :%v", msg)
		case logging.LevelError:
			msg = fmt.Sprintf("ERROR :%v", msg)
		default:
			panic(fmt.Sprintf("unknown level %v", lvl))
		}
		log.Println(append([]any{"msg", msg}, fields...))
	})
}

func check(addr string) int {
	n := time.Now()
	log.Printf("Running health check with timeout: %v", timeout)
	dialCtx, dialCancel := context.WithTimeout(context.Background(), timeout)
	defer dialCancel()
	log.Printf("Dialing: %s", addr)
	conn, err := grpc.DialContext(dialCtx, addr, grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("failed to connect to service at %q: %+v", addr, err)
		return connectionFailure
	}
	defer conn.Close()

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), timeout)
	defer rpcCancel()
	log.Printf("Check: %s", addr)
	resp, err := healthgrpc.NewHealthClient(conn).Check(rpcCtx, &healthgrpc.HealthCheckRequest{Service: ""})
	if err != nil {
		log.Printf("failed to do health rpc call: %+v", err)
		return rpcFailure
	}

	if resp.GetStatus() != healthgrpc.HealthCheckResponse_SERVING {
		log.Printf("service unhealthy (responded with %q)", resp.GetStatus().String())
		return unhealthy
	}
	log.Printf("status: %v. took: %vms", resp.GetStatus().String(), time.Now().Sub(n).Milliseconds())
	return 0
}
