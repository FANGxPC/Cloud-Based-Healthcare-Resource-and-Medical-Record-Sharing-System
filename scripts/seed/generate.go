package main

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"healthcloud/internal/models"
)

// All data below is SYNTHETIC: invented names, dates and conditions with no link to real people.
// The generator uses a fixed random seed, so every run produces the same data (repeatable demos).

var hospitalSeeds = []models.Hospital{
	{HospitalID: "H001", Name: "Northfield General Hospital", City: "Northfield", Address: "1 Synthetic Way", Phone: "+00 100 0001"},
	{HospitalID: "H002", Name: "Riverside Medical Centre", City: "Riverside", Address: "2 Example Road", Phone: "+00 100 0002"},
	{HospitalID: "H003", Name: "Lakeview Community Hospital", City: "Lakeview", Address: "3 Sample Street", Phone: "+00 100 0003"},
	{HospitalID: "H004", Name: "Hillcrest Specialty Hospital", City: "Hillcrest", Address: "4 Mock Avenue", Phone: "+00 100 0004"},
	{HospitalID: "H005", Name: "Eastgate Emergency Hospital", City: "Eastgate", Address: "5 Test Lane", Phone: "+00 100 0005"},
}

var (
	firstNames = []string{"Asha", "Ben", "Chen", "Divya", "Elena", "Farid", "Grace", "Hiro", "Isla", "Jonah",
		"Kavya", "Liam", "Maya", "Nikhil", "Olivia", "Pablo", "Quinn", "Riya", "Sam", "Tara", "Umar", "Vera", "Wen", "Yusuf", "Zara"}
	lastNames = []string{"Anders", "Bhatt", "Costa", "Dawson", "Evans", "Fischer", "Gupta", "Hughes", "Iyer", "Jensen",
		"Khan", "Lopez", "Mehta", "Novak", "Okafor", "Patel", "Quinn", "Rossi", "Singh", "Tanaka", "Usman", "Varga", "Walsh", "Xu", "Young"}
	bloodGroups = []string{"O+", "O-", "A+", "A-", "B+", "B-", "AB+", "AB-"}
	allergies   = []string{"Penicillin", "Peanuts", "Latex", "Sulfa drugs", "Shellfish", "Aspirin"}
	conditions  = []string{"Hypertension", "Type 2 diabetes", "Asthma", "COPD", "Atrial fibrillation",
		"Chronic kidney disease", "Pneumonia", "Migraine", "Osteoarthritis", "Hypothyroidism"}
)

// generateResources creates ICU beds, general beds and ventilators for each hospital.
// Some hospitals start below their alert threshold so the "low" badge appears in the demo.
func generateResources(r *rand.Rand, hospitals []models.Hospital, now string) []models.Resource {
	type spec struct{ minTotal, maxTotal, threshold int }
	specs := map[models.ResourceType]spec{
		models.ICUBed:     {8, 20, 3},
		models.GeneralBed: {40, 120, 10},
		models.Ventilator: {5, 15, 2},
	}
	var out []models.Resource
	for i, h := range hospitals {
		for _, t := range models.ResourceTypes {
			s := specs[t]
			total := s.minTotal + r.IntN(s.maxTotal-s.minTotal+1)
			available := r.IntN(total/2 + 1)
			if i == 1 && t == models.ICUBed { // H002 is nearly full: good for the race-safe demo
				available = 1
			}
			out = append(out, models.Resource{
				HospitalID: h.HospitalID, HospitalName: h.Name, ResourceType: t,
				Total: total, Available: available, Threshold: s.threshold,
				Version: 1, UpdatedAt: now, UpdatedBy: "seed",
			})
		}
	}
	return out
}

// generatePatient builds one synthetic patient. id is a Cognito sub for demo logins,
// or a random UUID for patients without an account.
func generatePatient(r *rand.Rand, id string, hospitals []models.Hospital, now string) models.Patient {
	pick := func(xs []string) string { return xs[r.IntN(len(xs))] }
	pickSome := func(xs []string, max int) []string {
		n := r.IntN(max + 1)
		out := []string{}
		for _, i := range r.Perm(len(xs))[:n] {
			out = append(out, xs[i])
		}
		return out
	}

	home := hospitals[r.IntN(len(hospitals))].HospitalID
	dob := time.Date(1940+r.IntN(80), time.Month(1+r.IntN(12)), 1+r.IntN(28), 0, 0, 0, 0, time.UTC)
	sex := []string{"F", "M", "X"}[r.IntN(3)]

	return models.Patient{
		PatientID:         id,
		FirstName:         pick(firstNames),
		LastName:          pick(lastNames),
		DateOfBirth:       dob.Format("2006-01-02"),
		Sex:               sex,
		BloodGroup:        pick(bloodGroups),
		Allergies:         pickSome(allergies, 2),
		Conditions:        pickSome(conditions, 3),
		EmergencyContact:  &models.EmergencyContact{Name: pick(firstNames) + " " + pick(lastNames), Phone: fmt.Sprintf("+00 200 %04d", r.IntN(10000))},
		HomeHospitalID:    home,
		CurrentHospitalID: home,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// deterministicUUID makes repeatable UUIDs from the seeded RNG, so re-running the seed updates
// the same patients instead of creating duplicates.
func deterministicUUID(r *rand.Rand) string {
	var b [16]byte
	for i := range b {
		b[i] = byte(r.IntN(256))
	}
	u, _ := uuid.FromBytes(b[:])
	return u.String()
}
