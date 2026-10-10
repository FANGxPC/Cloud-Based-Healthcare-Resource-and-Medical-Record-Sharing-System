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

// Fictional Indian hospitals (invented names, not real institutions).
var hospitalSeeds = []models.Hospital{
	{HospitalID: "H001", Name: "Sanjeevani Multispeciality Hospital", City: "Pune", Address: "14 Karve Road, Kothrud", Phone: "+91 20 4100 0001"},
	{HospitalID: "H002", Name: "Arogya Medical Centre", City: "Lucknow", Address: "22 Shahnajaf Road, Hazratganj", Phone: "+91 522 410 0002"},
	{HospitalID: "H003", Name: "Dhanvantari General Hospital", City: "Bengaluru", Address: "8 Hosur Road, Koramangala", Phone: "+91 80 4100 0003"},
	{HospitalID: "H004", Name: "Shushrusha Heart & Critical Care", City: "Chennai", Address: "31 Anna Salai, Teynampet", Phone: "+91 44 4100 0004"},
	{HospitalID: "H005", Name: "Jeevan Raksha Emergency Hospital", City: "Jaipur", Address: "5 Tonk Road, Malviya Nagar", Phone: "+91 141 410 0005"},
}

// Indian first names and surnames from across regions (combinations are random and fictional).
var (
	firstNames = []string{"Aarav", "Ananya", "Arjun", "Diya", "Ishaan", "Kavya", "Rohan", "Priya", "Vihaan", "Meera",
		"Aditya", "Sneha", "Karthik", "Lakshmi", "Rahul", "Pooja", "Siddharth", "Neha", "Vikram", "Anjali",
		"Farhan", "Ayesha", "Harpreet", "Gurpreet", "Joseph", "Tenzin", "Manish", "Deepika", "Suresh", "Kiran"}
	lastNames = []string{"Sharma", "Verma", "Iyer", "Reddy", "Nair", "Patel", "Gupta", "Singh", "Khan", "Das",
		"Mukherjee", "Banerjee", "Menon", "Pillai", "Joshi", "Kulkarni", "Deshmukh", "Chauhan", "Rao", "Agarwal",
		"Bhat", "Fernandes", "Saxena", "Mishra", "Chatterjee"}
	bloodGroups = []string{"O+", "O-", "A+", "A-", "B+", "B-", "AB+", "AB-"}
	allergies   = []string{"Penicillin", "Peanuts", "Latex", "Sulfa drugs", "Shellfish", "Aspirin", "Dust mites"}
	conditions  = []string{"Hypertension", "Type 2 diabetes", "Asthma", "COPD", "Atrial fibrillation",
		"Chronic kidney disease", "Pneumonia", "Migraine", "Osteoarthritis", "Hypothyroidism", "Anaemia", "Dengue (recovered)"}
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
		EmergencyContact:  &models.EmergencyContact{Name: pick(firstNames) + " " + pick(lastNames), Phone: fmt.Sprintf("+91 98%03d %05d", r.IntN(1000), r.IntN(100000))},
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
