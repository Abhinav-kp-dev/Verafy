// Seeds the practice fee schedule (example US dental office rates). Appointments are
// no longer hardcoded here — add one via Pre-Visit Notices → + Appointment (it
// auto-runs verification + the cost notice), after uploading patients via CSV.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Example fee schedule — typical US dental office fees (not a real practice's list).
var fees = []struct {
	code, desc string
	cents      int64
}{
	{"D0120", "Periodic oral evaluation", 6500},
	{"D0140", "Limited oral evaluation (problem focused)", 9500},
	{"D0150", "Comprehensive oral evaluation", 11000},
	{"D0210", "Full-mouth X-ray series", 15000},
	{"D0274", "Bitewing X-rays (four films)", 7500},
	{"D1110", "Adult cleaning (prophylaxis)", 12000},
	{"D1120", "Child cleaning (prophylaxis)", 8500},
	{"D1206", "Fluoride varnish", 4500},
	{"D1351", "Sealant, per tooth", 6000},
	{"D2140", "Amalgam filling, one surface", 18500},
	{"D2150", "Amalgam filling, two surfaces", 24000},
	{"D2391", "Composite filling, one surface (posterior)", 22000},
	{"D2392", "Composite filling, two surfaces (posterior)", 28500},
	{"D2740", "Crown, porcelain/ceramic", 125000},
	{"D2750", "Crown, porcelain fused to metal", 115000},
	{"D2950", "Core buildup, including pins", 32000},
	{"D3310", "Root canal, anterior tooth", 85000},
	{"D3330", "Root canal, molar", 120000},
	{"D4341", "Periodontal scaling & root planing, per quadrant", 27500},
	{"D4910", "Periodontal maintenance", 16000},
	{"D6010", "Implant placement", 245000},
	{"D6058", "Implant crown, porcelain/ceramic", 165000},
	{"D7140", "Simple tooth extraction", 22500},
	{"D7210", "Surgical tooth extraction", 38500},
	{"D8080", "Comprehensive orthodontic treatment (adolescent)", 550000},
	{"D9110", "Emergency palliative treatment", 12500},
	{"D9310", "Consultation", 9500},
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
	must(pool.QueryRow(ctx, `SELECT id FROM practices ORDER BY created_at LIMIT 1`).Scan(&practiceID))

	for _, f := range fees {
		_, err := pool.Exec(ctx, `INSERT INTO fee_schedule (practice_id, code, description, fee_cents) VALUES ($1,$2,$3,$4)
			ON CONFLICT (practice_id, code) DO UPDATE SET description=EXCLUDED.description, fee_cents=EXCLUDED.fee_cents`, practiceID, f.code, f.desc, f.cents)
		must(err)
	}
	fmt.Printf("fee schedule: %d procedures. Add appointments via Pre-Visit Notices -> + Appointment.\n", len(fees))
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "seed error:", err)
		os.Exit(1)
	}
}
