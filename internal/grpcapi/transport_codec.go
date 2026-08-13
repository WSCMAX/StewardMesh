package grpcapi

// Requirements: REQ-API-001, SEC-GUARD-001. Feature: integrations.protocols.

import (
	"fmt"
	"sync"

	stewardmeshv1 "github.com/maxlemke/stewardmesh/api/proto"
	"google.golang.org/grpc/codes"
	grpcencoding "google.golang.org/grpc/encoding"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// MaximumPublicMessageBytes bounds the three unauthenticated Guard request
// messages before protobuf unmarshalling. Their actual validated inputs are
// much smaller; this envelope leaves ample compatibility room without exposing
// the 34 MiB Exchange/Vault allocation boundary to unauthenticated callers.
const MaximumPublicMessageBytes = 64 << 10

type transportCodec struct {
	publicInputs map[protoreflect.FullName]struct{}
	rejected     *sync.Map
}

// TransportCodec returns the standard protobuf wire codec with a pre-unmarshal
// size boundary for public RPC inputs. It is intended for grpc.ForceServerCodec
// alongside Gateway.TapHandle and the global MaximumMessageBytes server limit.
func (g *Gateway) TransportCodec() grpcencoding.Codec {
	publicInputs := make(map[protoreflect.FullName]struct{})
	if g != nil {
		services := stewardmeshv1.File_stewardmesh_proto.Services()
		for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
			service := services.Get(serviceIndex)
			for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
				method := service.Methods().Get(methodIndex)
				fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
				if configured, ok := g.routes[fullMethod]; ok && configured.public {
					publicInputs[method.Input().FullName()] = struct{}{}
				}
			}
		}
	}
	var rejected *sync.Map
	if g != nil {
		rejected = &g.transportRejected
	}
	return transportCodec{publicInputs: publicInputs, rejected: rejected}
}

func (transportCodec) Marshal(value any) ([]byte, error) {
	message, ok := value.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("gRPC protobuf value has type %T", value)
	}
	return proto.Marshal(message)
}

func (codec transportCodec) Unmarshal(data []byte, value any) error {
	message, ok := value.(proto.Message)
	if !ok {
		return fmt.Errorf("gRPC protobuf value has type %T", value)
	}
	limit := MaximumMessageBytes
	if _, public := codec.publicInputs[message.ProtoReflect().Descriptor().FullName()]; public {
		limit = MaximumPublicMessageBytes
	}
	if len(data) > limit {
		if codec.rejected != nil {
			// grpc-go rewrites codec errors to Internal before the method handler
			// can preserve their status. Mark this exact request and let the
			// handler return ResourceExhausted without decoding attacker bytes.
			codec.rejected.Store(message, struct{}{})
			return nil
		}
		return status.Error(codes.ResourceExhausted, "protobuf request exceeds the gRPC transport limit")
	}
	return proto.UnmarshalOptions{DiscardUnknown: true, RecursionLimit: 64}.Unmarshal(data, message)
}

func (transportCodec) Name() string { return "proto" }
