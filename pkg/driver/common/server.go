/*
 * Copyright (c) 2020 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

package common

import (
	"context"
	"net"
	"os"
	"path"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/glog"
	"github.com/kubernetes-csi/csi-lib-utils/protosanitizer"
	uuid "github.com/satori/go.uuid"
	"google.golang.org/grpc"

	"github.com/baidubce/baiducloud-cce-csi-driver/pkg/util"
)

// Defines GRPC server interfaces
type GRPCServer interface {
	// Start services at the endpoint
	Start(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) error
}

func NewGRPCServer() GRPCServer {
	return &gRPCServer{}
}

// gRPC server
type gRPCServer struct {
	server *grpc.Server
}

func (s *gRPCServer) Start(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) error {
	return s.serve(endpoint, ids, cs, ns)
}

func (s *gRPCServer) serve(endpoint string, ids csi.IdentityServer, cs csi.ControllerServer, ns csi.NodeServer) error {
	proto, addr, err := util.ParseGRPCEndpoint(endpoint)
	if err != nil {
		glog.Fatal(err.Error())
	}

	if proto == "unix" {
		if !strings.HasPrefix(addr, "/") {
			addr = "/" + addr
		}

		if err := os.MkdirAll(path.Dir(addr), os.FileMode(0755)); err != nil && !os.IsExist(err) {
			glog.Fatalf("Failed to create csi endpoint dir, err: %v", err)
		}

		if err := os.Remove(addr); err != nil && !os.IsNotExist(err) {
			glog.Fatalf("Failed to remove %s, error: %s", addr, err.Error())
		}
	}

	listener, err := net.Listen(proto, addr)
	if err != nil {
		glog.Fatalf("Failed to listen: %v", err)
	}

	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(logGRPCCallWithTraceID),
	}
	server := grpc.NewServer(opts...)
	s.server = server

	if ids != nil {
		csi.RegisterIdentityServer(server, ids)
	}
	if cs != nil {
		csi.RegisterControllerServer(server, cs)
	}
	if ns != nil {
		csi.RegisterNodeServer(server, ns)
	}

	glog.Infof("Listening for connections on address: %#v", listener.Addr())
	return server.Serve(listener)
}

func logGRPCCallWithTraceID(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	ctx = context.WithValue(ctx, util.TraceIDKey, uuid.NewV4().String())
	glog.V(3).Infof("[%s] GRPC call: %s", ctx.Value(util.TraceIDKey), info.FullMethod)
	glog.V(5).Infof("[%s] GRPC request: %s", ctx.Value(util.TraceIDKey), protosanitizer.StripSecrets(req))
	resp, err := handler(ctx, req)
	if err != nil {
		glog.Errorf("[%s] GRPC error: %v", ctx.Value(util.TraceIDKey), err)
	} else {
		glog.V(5).Infof("[%s] GRPC response: %s", ctx.Value(util.TraceIDKey), protosanitizer.StripSecrets(resp))
	}
	return resp, err
}
