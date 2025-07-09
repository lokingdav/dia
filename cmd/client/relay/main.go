package main

import (
    "bufio"
    "context"
    "errors"
    "flag"
    "fmt"
    "io"
    "log"
    "os"
    "sync"
    "time"

    relaypb "github.com/dense-identity/denseid/api/go/relay/v1"
    configpkg "github.com/dense-identity/denseid/internal/config"
    "github.com/dense-identity/denseid/internal/signing"
    "github.com/google/uuid"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    "google.golang.org/protobuf/proto"
    "google.golang.org/protobuf/types/known/timestamppb"
)

// clientConfig holds our environment-loaded group keys.
type clientConfig struct {
    GPKStr string `env:"GROUP_PK,required"`
    USKStr string `env:"GROUP_USK,required"`
    GPK    []byte
    USK    []byte
}

func (c *clientConfig) ParseKeysAsBytes() error {
    if c == nil {
        return errors.New("config is nil")
    }
    var err error
    if c.GPK, err = signing.DecodeString(c.GPKStr); err != nil {
        return fmt.Errorf("decoding GROUP_PK: %w", err)
    }
    if c.USK, err = signing.DecodeString(c.USKStr); err != nil {
        return fmt.Errorf("decoding GROUP_USK: %w", err)
    }
    return nil
}

func main() {
    // Flags
    serverAddr := flag.String("server", "localhost:50051", "relay server address")
    channel := flag.String("channel", "", "channel/topic to join")
    senderID := flag.String("id", "", "unique client ID (randomized if empty)")
    flag.Parse()

    if *channel == "" {
        fmt.Fprintln(os.Stderr, "-channel is required")
        os.Exit(1)
    }
    if *senderID == "" {
        *senderID = uuid.NewString()
    }

    // Load env and config
    if err := configpkg.LoadEnv(".env.client"); err != nil {
        log.Printf("warning loading .env.client: %v", err)
    }
    cfg, err := configpkg.New[clientConfig]()
    if err != nil {
        log.Fatalf("loading config: %v", err)
    }
    if err := cfg.ParseKeysAsBytes(); err != nil {
        log.Fatalf("parsing keys: %v", err)
    }

    // Init group sig scheme (pairings, etc.)
    signing.InitGroupSignatures()

    conn, err := grpc.NewClient(
        *serverAddr,
        grpc.WithTransportCredentials(insecure.NewCredentials()),
    )
    if err != nil {
        log.Fatalf("grpc.NewClient(%q): %v", *serverAddr, err)
    }
    defer conn.Close()
    client := relaypb.NewRelayServiceClient(conn)

    // Subscribe in background
    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        defer wg.Done()

        // Build request
        req := &relaypb.SubscribeRequest{
            Channel:   *channel,
            SenderId:  *senderID,
            Timestamp: timestamppb.Now(),
        }
        // Sign it
        cloneReq := proto.Clone(req).(*relaypb.SubscribeRequest)
        cloneReq.Sigma = nil
        data, err := proto.MarshalOptions{Deterministic: true}.Marshal(cloneReq)
        if err != nil {
            log.Fatalf("marshal subscribe req: %v", err)
        }
        sig, err := signing.GrpSigSign(cfg.GPK, cfg.USK, data)
        if err != nil {
            log.Fatalf("sign subscribe req: %v", err)
        }
        req.Sigma = sig

        // Open stream
        stream, err := client.Subscribe(context.Background(), req)
        if err != nil {
            log.Fatalf("Subscribe RPC error: %v", err)
        }
        log.Printf("Subscribed to %q as %s", *channel, *senderID)

        // Read loop
        for {
            msg, err := stream.Recv()
            if err == io.EOF {
                log.Println("server closed subscribe stream")
                return
            }
            if err != nil {
                log.Fatalf("stream.Recv(): %v", err)
            }
            fmt.Printf(
                "[%s @ %s → %s] %q\n",
                msg.SenderId,
                msg.SentAt.AsTime().Format(time.RFC3339),
                msg.RelayAt.AsTime().Format(time.RFC3339),
                string(msg.Payload),
            )
        }
    }()

    // Publish loop
    scanner := bufio.NewScanner(os.Stdin)
    fmt.Println("Type messages to publish (Ctrl+D to exit):")
    for scanner.Scan() {
        text := scanner.Text()
        msg := &relaypb.RelayMessage{
            Id:       uuid.NewString(),
            Channel:  *channel,
            Payload:  []byte(text),
            SentAt:   timestamppb.Now(),
            SenderId: *senderID,
        }
        cloneMsg := proto.Clone(msg).(*relaypb.RelayMessage)
        cloneMsg.Sigma = nil
        cloneMsg.RelayAt = nil
        data, err := proto.MarshalOptions{Deterministic: true}.Marshal(cloneMsg)
        if err != nil {
            log.Printf("marshal publish msg: %v", err)
            continue
        }
        sig, err := signing.GrpSigSign(cfg.GPK, cfg.USK, data)
        if err != nil {
            log.Printf("sign publish msg: %v", err)
            continue
        }
        msg.Sigma = sig

        resp, err := client.Publish(context.Background(), msg)
        if err != nil {
            log.Printf("Publish RPC error: %v", err)
            continue
        }
        fmt.Printf("→ published at relay_at=%s\n", resp.RelayAt.AsTime().Format(time.RFC3339))
    }
    if err := scanner.Err(); err != nil {
        log.Printf("stdin error: %v", err)
    }

    log.Println("Shutting down client…")
    wg.Wait()
}
