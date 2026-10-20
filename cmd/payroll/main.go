// Command payroll is the batch entry point (migration step 4), driving
// internal/payroll.Main to produce payroll.txt and print the grand total.
package main

import (
	"context"
	"fmt"
	"os"

	"migrated-app/internal/payroll"
)

func main() {
	if err := payroll.Main(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "payroll:", err)
		os.Exit(1)
	}
}
