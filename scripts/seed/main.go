// Command seed loads SYNTHETIC data into DynamoDB and creates the demo Cognito users.
//
// Local (DynamoDB Local in Docker; creates the tables, no Cognito):
//
//	go run . -local
//
// AWS (after `sam deploy`; tables already exist). This writes to your AWS account:
//
//	SEED_PASSWORD='Demo-pass-123' go run . -stage dev -user-pool-id us-east-1_XXXX -region us-east-1
//
// Demo logins (password = $SEED_PASSWORD, never committed):
//
//	doctor.h001@example.com … doctor.h005@example.com   (Doctor at that hospital)
//	admin.h001@example.com  … admin.h005@example.com    (HospitalAdmin)
//	patient1@example.com    … patient3@example.com      (Patient; patientId = their Cognito sub)
//
// Re-running is safe: users are reused and records are overwritten with the same values.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	cip "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	ciptypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"

	"healthcloud/internal/db"
)

const (
	totalPatients = 50
	demoPatients  = 3
)

func main() {
	stage := flag.String("stage", "dev", "stack stage (table suffix)")
	local := flag.Bool("local", false, "seed DynamoDB Local at http://localhost:8000 and create tables")
	userPoolID := flag.String("user-pool-id", "", "Cognito user pool ID (AWS mode); empty = skip users")
	region := flag.String("region", "us-east-1", "AWS region")
	flag.Parse()

	ctx := context.Background()
	if *local {
		os.Setenv("DYNAMODB_ENDPOINT", "http://localhost:8000")
		// DynamoDB Local accepts any credentials. Set dummy ones so no real AWS profile is needed.
		os.Setenv("AWS_ACCESS_KEY_ID", "local")
		os.Setenv("AWS_SECRET_ACCESS_KEY", "local")
		*stage = "local"
	}
	os.Setenv("AWS_REGION", *region)

	client, err := db.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}
	tables := db.TablesForStage(*stage)
	if *local {
		if err := db.CreateTables(ctx, client, tables); err != nil {
			log.Fatal(err)
		}
		fmt.Println("✓ tables created in DynamoDB Local")
	}

	r := rand.New(rand.NewPCG(42, 2026)) // fixed seed: the same synthetic data every run
	now := db.Now()

	// 1. Hospitals
	hospitals := db.Hospitals{C: client, Table: tables.Hospitals}
	for _, h := range hospitalSeeds {
		h.CreatedAt = now
		must(hospitals.Put(ctx, h))
	}
	fmt.Printf("✓ %d hospitals\n", len(hospitalSeeds))

	// 2. Resources
	resources := db.Resources{C: client, Table: tables.Resources}
	rs := generateResources(r, hospitalSeeds, now)
	for _, x := range rs {
		must(resources.Put(ctx, x))
	}
	fmt.Printf("✓ %d resource rows\n", len(rs))

	// 3. Cognito users (AWS mode only). Patient logins return their sub, which becomes their patientId.
	var demoPatientIDs []string
	if *userPoolID != "" {
		password := os.Getenv("SEED_PASSWORD")
		if len(password) < 8 {
			log.Fatal("set SEED_PASSWORD (min 8 chars, upper+lower+digit) for the demo users")
		}
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(*region))
		if err != nil {
			log.Fatal(err)
		}
		u := userSeeder{c: cip.NewFromConfig(cfg), pool: *userPoolID, password: password}

		for _, h := range hospitalSeeds {
			id := strings.ToLower(h.HospitalID)
			u.ensure(ctx, "doctor."+id+"@example.com", "Doctor", h.HospitalID)
			u.ensure(ctx, "admin."+id+"@example.com", "HospitalAdmin", h.HospitalID)
		}
		for i := 1; i <= demoPatients; i++ {
			sub := u.ensure(ctx, fmt.Sprintf("patient%d@example.com", i), "Patient", "")
			demoPatientIDs = append(demoPatientIDs, sub)
		}
		fmt.Printf("✓ %d Cognito users\n", len(hospitalSeeds)*2+demoPatients)
	}

	// 4. Patients: demo logins first (ID = Cognito sub), then synthetic patients without logins.
	patients := db.Patients{C: client, Table: tables.Patients}
	for i := 0; i < totalPatients; i++ {
		id := deterministicUUID(r)
		if i < len(demoPatientIDs) {
			id = demoPatientIDs[i]
		}
		p := generatePatient(r, id, hospitalSeeds, now)
		if i < demoPatients {
			// Demo patients are always at H001 so the doctor.h001 demo finds them straight away.
			p.HomeHospitalID, p.CurrentHospitalID = "H001", "H001"
		}
		must(patients.Put(ctx, p))
	}
	fmt.Printf("✓ %d synthetic patients\n", totalPatients)
	fmt.Println("done.")
}

type userSeeder struct {
	c        *cip.Client
	pool     string
	password string
}

// ensure creates the user if missing, sets a permanent password (no "change password" step in
// the demo), adds the user to the group and returns their sub.
func (u userSeeder) ensure(ctx context.Context, email, group, hospitalID string) string {
	attrs := []ciptypes.AttributeType{
		{Name: aws.String("email"), Value: aws.String(email)},
		{Name: aws.String("email_verified"), Value: aws.String("true")},
	}
	if hospitalID != "" {
		attrs = append(attrs, ciptypes.AttributeType{Name: aws.String("custom:hospital_id"), Value: aws.String(hospitalID)})
	}

	_, err := u.c.AdminCreateUser(ctx, &cip.AdminCreateUserInput{
		UserPoolId:     aws.String(u.pool),
		Username:       aws.String(email),
		UserAttributes: attrs,
		MessageAction:  ciptypes.MessageActionTypeSuppress, // don't email example.com addresses
	})
	var exists *ciptypes.UsernameExistsException
	if err != nil && !errors.As(err, &exists) {
		log.Fatalf("create %s: %v", email, err)
	}

	_, err = u.c.AdminSetUserPassword(ctx, &cip.AdminSetUserPasswordInput{
		UserPoolId: aws.String(u.pool), Username: aws.String(email),
		Password: aws.String(u.password), Permanent: true,
	})
	if err != nil {
		log.Fatalf("set password %s: %v", email, err)
	}
	_, err = u.c.AdminAddUserToGroup(ctx, &cip.AdminAddUserToGroupInput{
		UserPoolId: aws.String(u.pool), Username: aws.String(email), GroupName: aws.String(group),
	})
	if err != nil {
		log.Fatalf("add %s to %s: %v", email, group, err)
	}

	out, err := u.c.AdminGetUser(ctx, &cip.AdminGetUserInput{UserPoolId: aws.String(u.pool), Username: aws.String(email)})
	if err != nil {
		log.Fatalf("get %s: %v", email, err)
	}
	for _, a := range out.UserAttributes {
		if aws.ToString(a.Name) == "sub" {
			return aws.ToString(a.Value)
		}
	}
	log.Fatalf("%s has no sub", email)
	return ""
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
