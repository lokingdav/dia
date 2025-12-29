package subscriber

import (
	"time"

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

// NewClient dials host:port with TLS (system roots) or plaintext.
func NewRelayClient(addr string, useTLS bool, extraOpts ...grpc.DialOption) (*RelayClient, error) {
	var creds grpc.DialOption
	if useTLS {
		creds = grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")) // system roots
	} else {
		creds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}

	kacp := keepalive.ClientParameters{
		Time:                5 * time.Minute,
		Timeout:             20 * time.Second,
		PermitWithoutStream: false,
	}

	opts := []grpc.DialOption{
		creds,
		grpc.WithKeepaliveParams(kacp),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(4*1024*1024),
			grpc.MaxCallSendMsgSize(4*1024*1024),
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
