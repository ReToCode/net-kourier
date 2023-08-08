/*
Copyright 2020 The Knative Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"fmt"
	"net"

	cluster "github.com/envoyproxy/go-control-plane/envoy/service/cluster/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/service/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/service/route/v3"
	"github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	xds "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
	health "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	grpcMaxConcurrentStreams = 1000000
)

type XdsServer struct {
	managementPort uint
	ctx            context.Context
	server         xds.Server
	snapshotCache  cache.SnapshotCache
}

func NewXdsServer(managementPort uint, callbacks xds.Callbacks) *XdsServer {
	ctx := context.Background()
	snapshotCache := cache.NewSnapshotCache(true, cache.IDHash{}, nil)
	srv := xds.NewServer(ctx, snapshotCache, callbacks)

	return &XdsServer{
		managementPort: managementPort,
		ctx:            ctx,
		server:         srv,
		snapshotCache:  snapshotCache,
	}
}

type healthServer struct {
	health.UnimplementedHealthServer
}

// Check implements the HealthServer interface.
func (healthServer) Check(context.Context, *health.HealthCheckRequest) (*health.HealthCheckResponse, error) {
	fmt.Println("healthServer.Check() called, returning", health.HealthCheckResponse_SERVING)
	return &health.HealthCheckResponse{Status: health.HealthCheckResponse_SERVING}, nil
}

// RunManagementServer starts an xDS server at the given Port.
func (envoyXdsServer *XdsServer) RunManagementServer() error {
	port := envoyXdsServer.managementPort
	server := envoyXdsServer.server

	grpcServer := grpc.NewServer(grpc.MaxConcurrentStreams(grpcMaxConcurrentStreams))
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}

	// register services
	fmt.Println("RegisterAggregatedDiscoveryServiceServer")
	discovery.RegisterAggregatedDiscoveryServiceServer(grpcServer, server)
	fmt.Println("RegisterHealthServer")
	health.RegisterHealthServer(grpcServer, healthServer{})
	fmt.Println("RegisterClusterDiscoveryServiceServer")
	cluster.RegisterClusterDiscoveryServiceServer(grpcServer, server)
	fmt.Println("RegisterListenerDiscoveryServiceServer")
	listener.RegisterListenerDiscoveryServiceServer(grpcServer, server)
	fmt.Println("RegisterRouteDiscoveryServiceServer")
	route.RegisterRouteDiscoveryServiceServer(grpcServer, server)
	fmt.Println("After register services")

	errCh := make(chan error)
	go func() {
		fmt.Println("xds_server.Serve()")
		if err = grpcServer.Serve(lis); err != nil {
			errCh <- err
		}
		fmt.Println("after channel: xds_server.Serve()")
	}()

	select {
	case <-envoyXdsServer.ctx.Done():
		fmt.Println("envoyXdsServer.ctx.Done() triggered")
		grpcServer.GracefulStop()
		return nil
	case err := <-errCh:
		fmt.Println("got signal on errCh", err)
		return fmt.Errorf("failed to serve: %w", err)
	}
}

func (envoyXdsServer *XdsServer) SetSnapshot(nodeID string, snapshot cache.ResourceSnapshot) error {
	fmt.Println("envoyXdsServer.SetSnapshot()", nodeID)
	return envoyXdsServer.snapshotCache.SetSnapshot(context.Background(), nodeID, snapshot)
}
