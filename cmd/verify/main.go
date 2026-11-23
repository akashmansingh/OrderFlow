package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/database"
	"github.com/theRiddler9/OrderFlow/internal/verify"
)

func main() {
	dbURL := flag.String("database-url", "", "PostgreSQL connection URL (defaults to DATABASE_URL)")
	flag.Parse()

	databaseURL := *dbURL
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "Error: database URL must be provided via -database-url or DATABASE_URL")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := database.Connect(ctx, databaseURL, 10*time.Second)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: connect to database failed: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	verifier := verify.NewVerifier(pool)
	results, allPassed := verifier.VerifyAll(ctx)

	fmt.Println("\n========================================================")
	fmt.Println("             DATABASE INVARIANT AUDIT REPORT            ")
	fmt.Println("========================================================")

	for _, r := range results {
		status := "[PASS]"
		if !r.Passed {
			status = "[FAIL]"
		}
		fmt.Printf("%s %s\n       %s\n", status, r.Name, r.Message)
		if r.Details != "" {
			fmt.Printf("       Details:\n%s\n", indent(r.Details, "         "))
		}
	}
	fmt.Println("--------------------------------------------------------")

	if allPassed {
		fmt.Println("RESULT: ALL 5 INVARIANTS PASSED (Zero anomalies, perfect conservation).")
		fmt.Println("========================================================")
		os.Exit(0)
	} else {
		fmt.Fprintln(os.Stderr, "RESULT: INVARIANT VIOLATION DETECTED! (See details above)")
		fmt.Fprintln(os.Stderr, "========================================================")
		os.Exit(1)
	}
}

func indent(text, prefix string) string {
	lines := ""
	for _, line := range splitLines(text) {
		lines += prefix + line + "\n"
	}
	return lines
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
