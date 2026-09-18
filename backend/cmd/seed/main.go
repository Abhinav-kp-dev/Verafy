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
}

var payers = []payer{
	{"Ameritas", "AMTAS00425", "35", "PPO", true},
	{"Anthem BCBS California", "84103", "35", "PPO", true},
	{"Cigna Dental", "62308", "35", "DHMO", true},
	{"MetLife Dental", "10134", "35", "PPO", true},
	{"UnitedHealthcare Dental", "52133", "35", "PPO", true},
	{"UnitedHealthcare", "87726", "30", "PPO", true}, // Stedi AAA error mocks live under this payer
	{"Delta Dental", "DDPA", "35", "PPO", false},     // no real-time 270/271 in test mode -> manual review path
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
			must(pool.QueryRow(ctx, `INSERT INTO payers (name, stedi_payer_id, supports_realtime, service_type_code, plan_type) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
				p.name, p.stediID, p.realtime, p.stc, p.planType).Scan(&id))
			fmt.Printf("created payer %-28s %s\n", p.name, p.stediID)
			created++
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
