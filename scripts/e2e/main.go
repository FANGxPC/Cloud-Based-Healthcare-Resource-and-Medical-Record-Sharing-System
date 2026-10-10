// Command e2e runs an end-to-end check against a DEPLOYED stack: real Cognito logins, the real
// API Gateway JWT authorizer, Lambdas, DynamoDB, SQS, S3. It proves the access rules and
// guarantees hold in the cloud, not just in unit tests.
//
//	cd scripts/e2e && go run .            (reads ../../frontend/.env.local and ../../.env.demo-users)
//
// It changes data (admissions, a transfer, a document) and then puts the bed counts back, so it can
// be re-run, e.g. live during the viva. Exit code 0 means every check passed.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	cip "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	ciptypes "github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider/types"
)

var failures int

func check(name string, ok bool, detail ...any) {
	if ok {
		fmt.Printf("  ✓ %s\n", name)
		return
	}
	failures++
	fmt.Printf("  ✗ %s %v\n", name, detail)
}

func section(s string) { fmt.Printf("\n▶ %s\n", s) }

// ---------------------------------------------------------------- config + login

func loadEnv(paths ...string) map[string]string {
	env := map[string]string{}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		s := bufio.NewScanner(f)
		for s.Scan() {
			line := strings.TrimSpace(strings.TrimPrefix(s.Text(), "\xef\xbb\xbf"))
			if k, v, ok := strings.Cut(line, "="); ok && !strings.HasPrefix(line, "#") {
				env[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		f.Close()
	}
	return env
}

type user struct {
	email, token, sub, hospital string
}

func login(ctx context.Context, c *cip.Client, pool, client, email, password string) user {
	out, err := c.AdminInitiateAuth(ctx, &cip.AdminInitiateAuthInput{
		UserPoolId: aws.String(pool), ClientId: aws.String(client),
		AuthFlow:       ciptypes.AuthFlowTypeAdminUserPasswordAuth,
		AuthParameters: map[string]string{"USERNAME": email, "PASSWORD": password},
	})
	if err != nil || out.AuthenticationResult == nil {
		fmt.Printf("login %s failed: %v\n", email, err)
		os.Exit(1)
	}
	u := user{email: email, token: aws.ToString(out.AuthenticationResult.IdToken)}
	var me struct {
		UserID     string  `json:"userId"`
		HospitalID *string `json:"hospitalId"`
	}
	api.call(u, "GET", "/me", nil, &me)
	u.sub = me.UserID
	if me.HospitalID != nil {
		u.hospital = *me.HospitalID
	}
	return u
}

// ---------------------------------------------------------------- HTTP

type apiClient struct{ base string }

var api apiClient

// call sends a request and decodes JSON into out (if not nil). It returns the status code.
func (a apiClient) call(u user, method, path string, body, out any) int {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, a.base+path, rdr)
	req.Header.Set("Authorization", "Bearer "+u.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("request failed:", err)
		return 0
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil {
		_ = json.Unmarshal(raw, out)
	}
	return res.StatusCode
}

type resource struct {
	HospitalID   string `json:"hospitalId"`
	ResourceType string `json:"resourceType"`
	Total        int    `json:"total"`
	Available    int    `json:"available"`
	Threshold    int    `json:"threshold"`
	Version      int    `json:"version"`
}

type patientSummary struct {
	PatientID         string          `json:"patientId"`
	CurrentHospitalID string          `json:"currentHospitalId"`
	Admission         json.RawMessage `json:"admission"`
}

func getResource(u user, h, t string) resource {
	var page struct{ Items []resource }
	api.call(u, "GET", "/hospitals/"+h+"/resources", nil, &page)
	for _, r := range page.Items {
		if r.ResourceType == t {
			return r
		}
	}
	return resource{}
}

// setAvailable is used by an admin of hospital h to set "available" (keeping total and threshold).
func setAvailable(admin user, h, t string, available int) int {
	r := getResource(admin, h, t)
	if available > r.Total {
		available = r.Total
	}
	return api.call(admin, "PUT", "/hospitals/"+h+"/resources/"+t,
		map[string]int{"total": r.Total, "available": available, "threshold": r.Threshold, "version": r.Version}, nil)
}

func unadmittedAt(doctor user, h string, exclude ...string) []string {
	var page struct{ Items []patientSummary }
	api.call(doctor, "GET", "/patients?hospitalId="+h+"&limit=100", nil, &page)
	var out []string
	for _, p := range page.Items {
		if (len(p.Admission) == 0 || string(p.Admission) == "null") && !contains(exclude, p.PatientID) {
			out = append(out, p.PatientID)
		}
	}
	return out
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- main

func main() {
	env := loadEnv("../../frontend/.env.local", "../../.env.demo-users")
	api = apiClient{base: env["VITE_API_URL"]}
	pool, clientID, password := env["VITE_USER_POOL_ID"], env["VITE_USER_POOL_CLIENT_ID"], env["DEMO_PASSWORD"]
	if api.base == "" || pool == "" || password == "" {
		fmt.Println("missing config: need frontend/.env.local and .env.demo-users")
		os.Exit(1)
	}
	ctx := context.Background()
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	cog := cip.NewFromConfig(cfg)

	section("Login (real Cognito ID tokens)")
	patient := login(ctx, cog, pool, clientID, "patient1@example.com", password)
	doctor := login(ctx, cog, pool, clientID, "doctor.h001@example.com", password)
	admin1 := login(ctx, cog, pool, clientID, "admin.h001@example.com", password)
	admin2 := login(ctx, cog, pool, clientID, "admin.h002@example.com", password)
	check("4 roles signed in; hospital claims H001/H001/H002", doctor.hospital == "H001" && admin1.hospital == "H001" && admin2.hospital == "H002")

	section("Authorization rules")
	check("Patient reads OWN record → 200", api.call(patient, "GET", "/patients/"+patient.sub, nil, nil) == 200)
	others := unadmittedAt(doctor, "H001", patient.sub)
	other := others[0]
	check("Patient reads ANOTHER patient → 403", api.call(patient, "GET", "/patients/"+other, nil, nil) == 403)
	check("Patient reads chain-wide resources → 403", api.call(patient, "GET", "/resources", nil, nil) == 403)
	check("HospitalAdmin reads a patient record → 403", api.call(admin1, "GET", "/patients/"+patient.sub, nil, nil) == 403)
	check("Admin H002 edits H001 resources → 403", setAvailable(admin2, "H001", "GENERAL_BED", 5) == 403)
	var all struct{ Items []resource }
	check("Doctor reads chain availability → 200, 15 rows", api.call(doctor, "GET", "/resources", nil, &all) == 200 && len(all.Items) == 15, len(all.Items))

	section("Audit: every doctor read is logged, and the patient can see it")
	before := time.Now().UTC().Add(-2 * time.Second).Format(time.RFC3339)
	check("Doctor (H001) opens patient1's record → 200", api.call(doctor, "GET", "/patients/"+patient.sub, nil, nil) == 200)
	var log struct {
		Items []struct{ ActorID, ActorHospitalID, Action, Timestamp string }
	}
	api.call(patient, "GET", "/patients/"+patient.sub+"/access-log", nil, &log)
	found := false
	for _, e := range log.Items {
		if e.ActorID == doctor.sub && e.Action == "VIEW_PROFILE" && e.ActorHospitalID == "H001" && e.Timestamp >= before {
			found = true
		}
	}
	check("patient1's access log shows that view (who, what, when, hospital)", found)
	var staff struct{ Items []json.RawMessage }
	check("Admin H001 sees staff accesses in /audit", api.call(admin1, "GET", "/audit", nil, &staff) == 200 && len(staff.Items) > 0)

	section("Optimistic locking on admin edits")
	r := getResource(admin1, "H001", "GENERAL_BED")
	body := map[string]int{"total": r.Total, "available": r.Available, "threshold": r.Threshold, "version": r.Version}
	check("Admin H001 saves with current version → 200", api.call(admin1, "PUT", "/hospitals/H001/resources/GENERAL_BED", body, nil) == 200)
	check("Saving again with the now-stale version → 409", api.call(admin1, "PUT", "/hospitals/H001/resources/GENERAL_BED", body, nil) == 409)

	section("Race safety on the live system: 1 free ICU bed, concurrent admissions")
	original := getResource(admin1, "H001", "ICU_BED").Available
	check("Admin sets H001 ICU available = 1", setAvailable(admin1, "H001", "ICU_BED", 1) == 200)
	candidates := others[1:]
	if len(candidates) > 8 {
		candidates = candidates[:8]
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		statuses = map[int]int{}
		winner   string
		start    = make(chan struct{})
	)
	for _, pid := range candidates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s := api.call(doctor, "POST", "/hospitals/H001/admissions", map[string]string{"patientId": pid, "resourceType": "ICU_BED"}, nil)
			mu.Lock()
			statuses[s]++
			if s == 201 {
				winner = pid
			}
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()
	fmt.Printf("    %d simultaneous requests → %v\n", len(candidates), statuses)
	check("exactly one 201, all others 409", statuses[201] == 1 && statuses[409] == len(candidates)-1, statuses)
	check("available is now 0 (never negative)", getResource(admin1, "H001", "ICU_BED").Available == 0)
	if winner != "" {
		check("discharge the winner → 204", api.call(doctor, "DELETE", "/hospitals/H001/admissions/"+winner, nil, nil) == 204)
	}
	check("restore original ICU availability", setAvailable(admin1, "H001", "ICU_BED", original) == 200)

	section("Emergency transfer: API → SQS → worker → admin decision")
	setAvailable(admin2, "H002", "ICU_BED", max(1, getResource(admin2, "H002", "ICU_BED").Available))
	h2Before := getResource(admin2, "H002", "ICU_BED").Available
	var tr struct{ TransferID, Status string }
	s := api.call(doctor, "POST", "/transfers", map[string]string{
		"patientId": other, "toHospitalId": "H002", "resourceType": "ICU_BED", "priority": "CRITICAL", "reason": "e2e synthetic test",
	}, &tr)
	check("Doctor H001 requests transfer → 202 QUEUED", s == 202 && tr.Status == "QUEUED", s, tr.Status)
	deadline := time.Now().Add(30 * time.Second)
	for tr.Status == "QUEUED" && time.Now().Before(deadline) {
		time.Sleep(1500 * time.Millisecond)
		api.call(doctor, "GET", "/transfers/"+tr.TransferID, nil, &tr)
	}
	check("worker held a bed → PENDING_APPROVAL", tr.Status == "PENDING_APPROVAL", tr.Status)
	check("H002 ICU available decreased by 1 (bed held)", getResource(admin2, "H002", "ICU_BED").Available == h2Before-1)
	check("origin admin (H001) cannot decide → 403", api.call(admin1, "POST", "/transfers/"+tr.TransferID+"/decision", map[string]string{"decision": "ACCEPT"}, nil) == 403)
	check("destination admin (H002) accepts → 200", api.call(admin2, "POST", "/transfers/"+tr.TransferID+"/decision", map[string]string{"decision": "ACCEPT"}, &tr) == 200 && tr.Status == "ACCEPTED", tr.Status)
	check("accepting twice → 409", api.call(admin2, "POST", "/transfers/"+tr.TransferID+"/decision", map[string]string{"decision": "ACCEPT"}, nil) == 409)
	var moved patientSummary
	api.call(doctor, "GET", "/patients/"+other, nil, &moved)
	check("patient now at H002", moved.CurrentHospitalID == "H002", moved.CurrentHospitalID)
	check("cleanup: H002 discharges the patient → 204", api.call(admin2, "DELETE", "/hospitals/H002/admissions/"+other, nil, nil) == 204)

	section("Documents: presigned upload → S3 → presigned download")
	content := []byte("SYNTHETIC lab report generated by the e2e test at " + time.Now().UTC().Format(time.RFC3339))
	var created struct {
		Document  struct{ DocumentID string }
		UploadURL string
	}
	s = api.call(doctor, "POST", "/patients/"+patient.sub+"/documents", map[string]any{
		"title": "E2E lab report", "category": "LAB_REPORT", "fileName": "e2e-report.txt", "contentType": "text/plain", "sizeBytes": len(content),
	}, &created)
	check("Doctor gets presigned PUT URL → 201", s == 201 && created.UploadURL != "", s)
	put, _ := http.NewRequest("PUT", created.UploadURL, bytes.NewReader(content))
	put.Header.Set("Content-Type", "text/plain")
	pres, err := http.DefaultClient.Do(put)
	check("file PUT directly to S3 (bypassing Lambda) → 200", err == nil && pres.StatusCode == 200)
	if pres != nil {
		pres.Body.Close()
	}
	docPath := "/patients/" + patient.sub + "/documents/" + created.Document.DocumentID
	check("Doctor completes upload → 200", api.call(doctor, "POST", docPath+"/complete", nil, nil) == 200)
	var got struct{ DownloadURL string }
	check("Patient gets presigned GET URL for own document → 200", api.call(patient, "GET", docPath, nil, &got) == 200)
	if got.DownloadURL != "" {
		res, err := http.Get(got.DownloadURL)
		var downloaded []byte
		if err == nil {
			downloaded, _ = io.ReadAll(res.Body)
			res.Body.Close()
		}
		check("downloaded bytes match the upload", bytes.Equal(downloaded, content))
	}
	plainGet, _ := http.Get("https://" + strings.Split(strings.TrimPrefix(created.UploadURL, "https://"), "?")[0])
	check("the same object WITHOUT a signature → 403 (bucket is private)", plainGet != nil && plainGet.StatusCode == 403)

	fmt.Println()
	if failures > 0 {
		fmt.Printf("✗ %d check(s) FAILED\n", failures)
		os.Exit(1)
	}
	fmt.Println("✓ ALL CHECKS PASSED")
}
