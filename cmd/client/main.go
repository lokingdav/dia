package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	// Import the generated protobuf code.
	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	didconfig "github.com/dense-identity/denseid/internal/config"
	"github.com/dense-identity/denseid/internal/signing"
)

// result holds the outcome of a single gRPC call.
type result struct {
	response *pb.EnrollmentResponse
	err      error
	server   string
}

type keypairs struct {
	PublicKeys  []string
	PrivateKeys []string
}

type config struct {
	TN          string `env:"TN,required"`
	NBio        uint32 `env:"N_BIO" envDefault:"0"`
	PublicKeys  string `env:"PUBLIC_KEYS,required"`
	PrivateKeys string `env:"PRIVATE_KEYS,required"`

	DisplayName    string `env:"DISPLAY_NAME,required"`
	DisplayLogoUrl string `env:"DISPLAY_LOGO_URL"`

	EnrollmentServer1Url       string `env:"ENROLLMENT_SERVER_1_URL"`
	EnrollmentServer1PublicKey string `env:"ENROLLMENT_SERVER_1_PUBLIC_KEY"`

	EnrollmentServer2Url       string `env:"ENROLLMENT_SERVER_2_URL"`
	EnrollmentServer2PublicKey string `env:"ENROLLMENT_SERVER_2_PUBLIC_KEY"`
}

func (c *config) getKeypairs() (keypairs, error) {
	pks, sks := strings.Split(c.PublicKeys, ","), strings.Split(c.PrivateKeys, ",")
	if len(pks) != len(sks) {
		return keypairs{}, fmt.Errorf("Private-Public key lengths do not match")
	}

	publicKeys, privateKeys := []string{}, []string{}
	for i := 0; i < len(pks); i++ {
		if _, err := signing.DecodeString(sks[i]); err == nil {
			if _, err := signing.DecodeString(pks[i]); err == nil {
				privateKeys = append(privateKeys, sks[i])
				publicKeys = append(publicKeys, pks[i])
			}
		}
	}

	return keypairs{PublicKeys: publicKeys, PrivateKeys: privateKeys}, nil
}

func (cfg *config) newEnrollmentRequest() (*pb.EnrollmentRequest, error) {
	kps, err := cfg.getKeypairs()
	if err != nil {
		return nil, fmt.Errorf("Private-Public key lengths do not match %v", err)
	}

	var data = &pb.EnrollmentRequest{
		Tn:         cfg.TN,
		PublicKeys: kps.PublicKeys,
		NBio:       cfg.NBio,
		Iden: &pb.DisplayInformation{
			Name:    cfg.DisplayName,
			LogoUrl: cfg.DisplayLogoUrl,
		},
	}

	dataBytes, err := proto.Marshal(data)
	if err != nil {
		return nil, err
	}
	authSigs := []string{}
	for i := 0; i < len(kps.PrivateKeys); i++ {
		sk, _ := signing.DecodeString(kps.PrivateKeys[i])
		sig := signing.Sign(sk, dataBytes)
		authSigs = append(authSigs, signing.EncodeToString(sig))
	}

	data.AuthSigs = authSigs

	return data, nil
}

func main() {
	didconfig.LoadEnv(".env.client")
	cfg, err := didconfig.New[config]()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	request, err := cfg.newEnrollmentRequest()
	if err != nil {
		log.Fatalf("Failed to create enrollment request: %v", err)
	}

	var serverAddresses = []string{
		cfg.EnrollmentServer1Url,
		cfg.EnrollmentServer2Url,
	}

	var wg sync.WaitGroup
	// Create a channel to receive results from the goroutines.
	// The buffer size matches the number of requests.
	resultsChan := make(chan result, len(serverAddresses))

	for _, addr := range serverAddresses {
		wg.Add(1)
		go func(serverAddress string) {
			defer wg.Done()

			conn, err := grpc.NewClient(serverAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				resultsChan <- result{err: err, server: serverAddress}
				return
			}
			defer conn.Close()

			client := pb.NewEnrollmentServiceClient(conn)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
			defer cancel()

			response, err := client.EnrollSubscriber(ctx, request)
			// Send the outcome (success or error) to the channel.
			resultsChan <- result{response: response, err: err, server: serverAddress}
		}(addr)
	}

	// Wait for all goroutines to finish, then close the channel.
	wg.Wait()
	close(resultsChan)

	// --- Process all results in the main goroutine ---
	log.Println("--- Processing all server responses ---")
	var successfulResponses []*pb.EnrollmentResponse
	for res := range resultsChan {
		if res.err != nil {
			log.Printf("ERROR from %s: %v", res.server, res.err)
		} else {
			log.Printf("Success from %s: Enrollment ID = %s", res.server, res.response.GetEid())
			successfulResponses = append(successfulResponses, res.response)
		}
	}

	// Now you can work with the collected responses.
	log.Printf("--- Finished. Received %d successful responses. ---", len(successfulResponses))
}
