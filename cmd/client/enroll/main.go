package main

import (
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"log"
	mr "math/rand/v2"

	// Import the generated protobuf code.
	pb "github.com/dense-identity/denseid/api/go/enrollment/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"

	"github.com/dense-identity/denseid/internal/amf"
	"github.com/dense-identity/denseid/internal/signing"
	"github.com/dense-identity/denseid/internal/voprf"
)

type newEnrollment struct {
	request        *pb.EnrollmentRequest
	isk            []byte
	ipk            []byte
	amfSk          []byte
	amfPk          []byte
	blindedTickets []voprf.BlindedTicket
}

func createNewEnrollment(phoneNumber, displayName, logoUrl string) (*newEnrollment, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	isk, ipk, err := signing.RegSigKeyGen()
	if err != nil {
		return nil, err
	}

	amfSk, amfPk, err := amf.Keygen()
	if err != nil {
		return nil, err
	}

	blindedTickets := voprf.GenerateTickets(1)
	blinded := make([][]byte, len(blindedTickets))
	for i, v := range blindedTickets {
		blinded[i] = v.Blinded
	}

	derIpk, err := signing.ExportPublicKeyToDER(ipk)
	if err != nil {
		return nil, fmt.Errorf("failed to export public key to DER format: %w", err)
	}

	var req = &pb.EnrollmentRequest{
		Tn:   phoneNumber,
		NBio: 0,
		Iden: &pb.DisplayInformation{
			Name:    displayName,
			LogoUrl: logoUrl,
		},
		Nonce:          signing.EncodeToHex(nonce),
		Ipk:            derIpk,
		Pk:             amfPk,
		BlindedTickets: blinded,
	}

	data, err := proto.Marshal(req)
	if err != nil {
		return nil, err
	}
	req.Sigma = signing.RegSigSign(isk, data)

	payload := newEnrollment{
		isk:            isk,
		ipk:            ipk,
		amfSk:          amfSk,
		amfPk:          amfPk,
		blindedTickets: blindedTickets,
		request:        req,
	}

	return &payload, nil
}

func createGRPCClient(addr string) pb.EnrollmentServiceClient {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("grpc.NewClient(%q): %v", addr, err)
	}
	return pb.NewEnrollmentServiceClient(conn)
}

func main() {
	serverAddr := flag.String("host", "localhost:50051", "Server address")
	phone := flag.String("phone", "", "Phone number to be enrolled with")
	name := flag.String("name", "", "Name of subscriber")
	logoUrl := flag.String("logo", "", "Logo url")
	flag.Parse()

	if *name == "" {
		log.Fatal("--name is required")
	}
	if *phone == "" {
		log.Fatal("--phone is required")
	}
	if *logoUrl == "" {
		*logoUrl = fmt.Sprintf("https://avatar.iran.liara.run/public/%d", mr.IntN(55))
	}

	data, err := createNewEnrollment(*phone, *name, *logoUrl)
	if err != nil {
		log.Fatalf("Error creating enrollment data: %v", err)
	}

	client := createGRPCClient(*serverAddr)

	res, err := client.EnrollSubscriber(context.Background(), data.request)
	if err != nil {
		log.Fatalf("Error subscribing: %v", err)
	}

	tickets := voprf.FinalizeTickets(data.blindedTickets, res.EvaluatedTickets)

	// Marshal the expiration timestamp properly
	expirationBytes, err := proto.Marshal(res.GetExp())
	if err != nil {
		log.Fatalf("Error marshaling expiration: %v", err)
	}

	env := make([]string, 0)

	env = append(env,
		fmt.Sprintf("\nMY_PHONE=%s", *phone),
		fmt.Sprintf("MY_NAME=\"%s\"", *name),
		fmt.Sprintf("MY_LOGO=\"%s\"", *logoUrl),

		fmt.Sprintf("\nENROLLMENT_ID=%s", res.GetEid()),
		fmt.Sprintf("ENROLLMENT_EXPIRATION=%x", expirationBytes),
		fmt.Sprintf("RA_PUBLIC_KEY=%x", res.GetEpk()),
		fmt.Sprintf("RA_SIGNATURE=%x", res.GetSigma()),

		fmt.Sprintf("\nRUA_PRIVATE_KEY=%x", data.amfSk),
		fmt.Sprintf("RUA_PUBLIC_KEY=%x", data.amfPk),

		fmt.Sprintf("\nSUBSCRIBER_PRIVATE_KEY=%x", data.isk),
		fmt.Sprintf("SUBSCRIBER_PUBLIC_KEY=%x", data.ipk),

		fmt.Sprintf("\nACCESS_TICKET_VK=%x", res.Avk),
		fmt.Sprintf("\nSAMPLE_TICKET=%x", tickets[0].ToBytes()),

		fmt.Sprintf("MODERATOR_PUBLIC_KEY=%x", res.Mpk),
	)

	for i := range env {
		fmt.Println(env[i])
	}
}
