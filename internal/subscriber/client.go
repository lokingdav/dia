package subscriber

import (
	"fmt"
	"time"

	relaypb "github.com/dense-identity/denseid/api/go/relay/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Client owns the gRPC connection and RelayService stub.
type Client struct {
	conn *grpc.ClientConn
	Stub relaypb.RelayServiceClient
}

// NewClient dials host:port with TLS (system roots) or plaintext.
func NewClient(host string, port int, useTLS bool, extraOpts ...grpc.DialOption) (*Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)

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

	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{
		conn: conn,
		Stub: relaypb.NewRelayServiceClient(conn),
	}, nil
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	return c.conn.Close()
}
