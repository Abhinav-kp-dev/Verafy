// Clears all verification data (jobs, attempts, batches, queue) but keeps payers and patients.
// Use before a demo:  go run ./cmd/reset
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL required")
		os.Exit(1)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE patient_notices, job_attempts, jobs, batches, river_job RESTART IDENTITY CASCADE`); err != nil {
		fmt.Fprintln(os.Stderr, "reset failed:", err)
		os.Exit(1)
	}
	fmt.Println("reset complete: jobs, attempts, batches, notices and queue cleared (payers, patients, appointments, fees kept)")
}
