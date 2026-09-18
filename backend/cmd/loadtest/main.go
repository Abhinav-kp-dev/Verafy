// Synthetic load: enqueues N verification jobs (cycling over seeded patients) through
// the running API and prints live batch counters until the batch drains.
//
//	go run ./cmd/loadtest -n 10000 -api http://localhost:8080
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

type batch struct {
	ID             string `json:"id"`
	TotalJobs      int    `json:"totalJobs"`
	Queued         int    `json:"queued"`
	Processing     int    `json:"processing"`
	Verified       int    `json:"verified"`
	GapFlagged     int    `json:"gapFlagged"`
	Retrying       int    `json:"retrying"`
	NeedsReview    int    `json:"needsReview"`
	ManualResolved int    `json:"manualResolved"`
}

func main() {
	n := flag.Int("n", 10000, "number of jobs")
	api := flag.String("api", "http://localhost:8080", "API base URL")
	flag.Parse()

	body, _ := json.Marshal(map[string]int{"count": *n})
	start := time.Now()
	resp, err := http.Post(*api+"/api/batches/synthetic", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var b batch
	json.NewDecoder(resp.Body).Decode(&b)
	resp.Body.Close()
	if b.ID == "" {
		fmt.Fprintln(os.Stderr, "failed to create batch, status", resp.Status)
		os.Exit(1)
	}
	fmt.Printf("batch %s accepted with %d jobs in %s (API returned 202 immediately)\n\n", b.ID[:8], b.TotalJobs, time.Since(start).Round(time.Millisecond))

	for {
		r, err := http.Get(*api + "/api/batches/" + b.ID + "?limit=0")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var out struct {
			Batch batch `json:"batch"`
		}
		json.NewDecoder(r.Body).Decode(&out)
		r.Body.Close()
		x := out.Batch
		done := x.Verified + x.GapFlagged + x.NeedsReview + x.ManualResolved
		fmt.Printf("\r%6.1fs  queued %-6d processing %-4d retrying %-4d | verified %-6d gap %-5d review %-5d | %5.1f%%   ",
			time.Since(start).Seconds(), x.Queued, x.Processing, x.Retrying, x.Verified, x.GapFlagged, x.NeedsReview, float64(done)/float64(x.TotalJobs)*100)
		if done >= x.TotalJobs {
			fmt.Printf("\n\ncompleted %d jobs in %s (%.1f jobs/s)\n", x.TotalJobs, time.Since(start).Round(time.Second), float64(x.TotalJobs)/time.Since(start).Seconds())
			return
		}
		time.Sleep(time.Second)
	}
}
