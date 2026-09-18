// Seeds the practice and the Stedi-supported payer directory. Patient data is no
// longer hardcoded here — upload patients via a CSV through Batch Upload
// (see coveragecheck/seed-data/patients.csv for a ready-made set using Stedi's
// documented mock subscribers) or the Verify / Patients screens.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type payer struct {
	name, stediID, stc, planType string
	realtime                     bool
	phone, ivr                   string // voice path for payers without real-time EDI
}

var payers = []payer{
	{"Ameritas", "AMTAS00425", "35", "PPO", true, "", ""},
	{"Anthem BCBS California", "84103", "35", "PPO", true, "", ""},
	{"Cigna Dental", "62308", "35", "DHMO", true, "", ""},
	{"MetLife Dental", "10134", "35", "PPO", true, "", ""},
	{"UnitedHealthcare Dental", "52133", "35", "PPO", true, "", ""},
	{"UnitedHealthcare", "87726", "30", "PPO", true, "", ""}, // Stedi AAA error mocks live under this payer
	// No real-time 270/271 in test mode. With a provider-services line on file the AI voice
	// agent calls instead of routing straight to manual review.
	{"Delta Dental", "DDPA", "35", "PPO", false, "+18005240149", "Press 2 for dental providers, then 1 for eligibility and benefits; say 'representative' to skip the automated benefits read-back."},
	{"Guardian Dental", "GRDN", "35", "PPO", false, "+18005414254", "Provider services menu: option 3 for eligibility. Have the NPI ready."},
	{"Principal Dental", "PRNC", "35", "PPO", false, "", ""}, // deliberately no phone: stays on the plain manual-review path
}

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL required")
		os.Exit(1)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	must(err)
	defer pool.Close()

	var practiceID uuid.UUID
	err = pool.QueryRow(ctx, `SELECT id FROM practices ORDER BY created_at LIMIT 1`).Scan(&practiceID)
	if err != nil {
		must(pool.QueryRow(ctx, `INSERT INTO practices (name) VALUES ($1) RETURNING id`, "Riverside Dental Care").Scan(&practiceID))
		fmt.Println("created practice Riverside Dental Care")
	}

	created := 0
	for _, p := range payers {
		var id uuid.UUID
		err := pool.QueryRow(ctx, `SELECT id FROM payers WHERE stedi_payer_id=$1`, p.stediID).Scan(&id)
		if err != nil {
			must(pool.QueryRow(ctx, `INSERT INTO payers (name, stedi_payer_id, supports_realtime, service_type_code, plan_type, provider_services_phone, ivr_notes) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),NULLIF($7,'')) RETURNING id`,
				p.name, p.stediID, p.realtime, p.stc, p.planType, p.phone, p.ivr).Scan(&id))
			fmt.Printf("created payer %-28s %s\n", p.name, p.stediID)
			created++
		} else if p.phone != "" {
			// idempotent: keep the voice directory current for payers that already exist
			_, err := pool.Exec(ctx, `UPDATE payers SET provider_services_phone=$2, ivr_notes=NULLIF($3,'') WHERE id=$1 AND provider_services_phone IS DISTINCT FROM $2`, id, p.phone, p.ivr)
			must(err)
		}
	}
	fmt.Printf("seed complete: %d payers (%d new). Upload patients via CSV in Batch Upload — see seed-data/patients.csv\n", len(payers), created)
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed error:", err)
		os.Exit(1)
	}
}
