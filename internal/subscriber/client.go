package subscriber

import (
	"fmt"
	"time"

	keyderivationpb "github.com/dense-identity/denseid/api/go/keyderivation/v1"
	relaypb "github.com/dense-identity/denseid/api/go/relay/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Client owns the gRPC connection and RelayService stub.
type RelayClient struct {
	conn *grpc.ClientConn
	Stub relaypb.RelayServiceClient
}

type KeyDeriveClient struct {
	conn *grpc.ClientConn
	Stub keyderivationpb.KeyDerivationServiceClient
}

// NewClient dials host:port with TLS (system roots) or plaintext.
func NewRelayClient(addr string, useTLS bool, extraOpts ...grpc.DialOption) (*RelayClient, error) {
	var creds grpc.DialOption
	if useTLS {
		creds = grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")) // system roots
	} else {
		creds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	kacp := keepalive.ClientParameters{
		Time:                30 * time.Second, // send pings every 30s if idle
		Timeout:             10 * time.Second, // wait 10s for ping ack
		PermitWithoutStream: true,             // keepalive even with no active RPCs
	}

	opts := []grpc.DialOption{
		creds,
		grpc.WithKeepaliveParams(kacp),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(4 * 1024 * 1024),
			grpc.MaxCallSendMsgSize(4 * 1024 * 1024),
		),
	}
	opts = append(opts, extraOpts...)

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, err
	}
	return &RelayClient{
		conn: conn,
		Stub: relaypb.NewRelayServiceClient(conn),
	}, nil
}

// Close closes the underlying connection.
func (c *RelayClient) Close() error {
	return c.conn.Close()
}

func NewKeyDeriveClient(addr string) (*KeyDeriveClient, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc.NewClient(%q): %v", addr, err)
	}
	return &KeyDeriveClient{
		conn: conn,
		Stub: keyderivationpb.NewKeyDerivationServiceClient(conn),
	}, nil
}

func (c *KeyDeriveClient) Close() error {
	return c.conn.Close()
}