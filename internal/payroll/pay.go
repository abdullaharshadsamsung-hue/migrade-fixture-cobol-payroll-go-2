// Package payroll migrates the COBOL PAYROLL batch program.
//
// It reads employee records (4-digit id, space, 3-digit hours, space,
// 5-digit hourly rate in cents) from employees.txt, computes each
// employee's pay with time-and-a-half overtime over 40 hours, writes one
// line per employee to payroll.txt, and reports a grand total on stdout.
//
// All money arithmetic is done in integer cents; rounding is explicitly
// round-half-up to match COBOL's ROUNDED clause. Output formatting
// reproduces the original edited pictures exactly:
//
//	WS-PAY-OUT   PIC Z(6)9.99  -> "  1234.56" (7 wide, blank-suppressed)
//	WS-TOTAL-OUT PIC Z(8)9.99  -> "  12345.67" (9 wide, blank-suppressed)
package payroll

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// MaxStandardHours is the threshold above which hours are paid at
// time-and-a-half, matching the original program's overtime rule.
const MaxStandardHours = 40

// OvertimeMultiplier is the time-and-a-half factor applied to hours
// beyond MaxStandardHours, scaled by 2 for exact integer arithmetic.
const OvertimeMultiplier = 3 // 1.5 * 2; divide intermediate sum by 2

// Employee is one parsed input record.
//
// ID is kept as a string so leading zeros (e.g. "0001") survive exactly
// as they appear in the input file and in the output line.
type Employee struct {
	ID         string
	Hours      int
	HourlyRate int // in integer cents, as read from the file
}

// computePay returns the pay for one employee in integer cents, using
// time-and-a-half for hours over MaxStandardHours.
//
// COBOL: COMPUTE WS-PAY ROUNDED = 40 * WS-RATE + OVERTIME * WS-RATE * 1.5
// where WS-RATE has an implied decimal V99. Working in cents and
// rounding half-up reproduces that arithmetic exactly:
//
//	pay = round_half_up( (hours*rate) / 100 )                  if hours <= 40
//	pay = round_half_up( (2*(40*rate) + 3*(ot*rate)) / 200 )   otherwise
func computePay(hours, hourlyRate int) int {
	if hours <= MaxStandardHours {
		return roundHalfUp(hours*hourlyRate, 100)
	}
	over := hours - MaxStandardHours
	numerator := 2*(MaxStandardHours*hourlyRate) + OvertimeMultiplier*(over*hourlyRate)
	return roundHalfUp(numerator, 200)
}

// roundHalfUp divides num by den and rounds halves away from zero, which
// is what COBOL's ROUNDED clause does for positive values used here.
func roundHalfUp(num, den int) int {
	q, r := num/den, num%den
	if r*2 >= den {
		q++
	}
	return q
}

// FormatCents renders cents as a blank-suppressed fixed two-decimal
// number, matching the edited pictures used in the COBOL program.
//
// The value occupies exactly width characters: the integer part is
// blank-padded on the left (zero suppression), followed by '.' and two
// digits. It matches PIC Z(6)9.99 when width == 7 and PIC Z(8)9.99 when
// width == 9.
//
// MIGRATION_NOTE: the source edited pictures carry no sign position, so
// negative amounts are not representable there; Go ints can go negative
// on malformed input, so a leading '-' is emitted defensively outside the
// fixed width in that case.
func FormatCents(cents, width int) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}
	s := strconv.Itoa(cents)
	if len(s) < 3 {
		s = strings.Repeat("0", 3-len(s)) + s
	}
	intPart := s[:len(s)-2]
	frac := s[len(s)-2:]
	if len(intPart) > width-3 {
		// Value exceeds the edited picture's capacity; COBOL would
		// silently truncate the high-order digits — mirror that so the
		// output width stays stable.
		intPart = intPart[len(intPart)-(width-3):]
	}
	out := strings.Repeat(" ", width-3-len(intPart)) + intPart + "." + frac
	if neg {
		return "-" + out
	}
	return out
}

// parseRecord parses one fixed-width employee line:
//
//	pos 0..3   : 4-digit employee id
//	pos 4      : single space (FILLER)
//	pos 5..7   : 3-digit hours
//	pos 8      : single space (FILLER)
//	pos 9..13  : 5-digit hourly rate in cents
//
// It is lenient about trailing whitespace and rejects lines that are too
// short, contain non-digit characters in the numeric fields, or whose
// separator positions are not spaces.
func parseRecord(line string) (Employee, error) {
	trimmed := strings.TrimRight(line, " \t\r\n")
	if len(trimmed) < 14 {
		return Employee{}, fmt.Errorf("record too short (%d chars, need at least 14): %q", len(trimmed), line)
	}
	for _, pos := range []int{4, 8} {
		if trimmed[pos] != ' ' {
			return Employee{}, fmt.Errorf("record separator at position %d is not a space: %q", pos, line)
		}
	}
	id := trimmed[0:4]
	hours, err := strconv.Atoi(trimmed[5:8])
	if err != nil {
		return Employee{}, fmt.Errorf("invalid hours %q: %w", trimmed[5:8], err)
	}
	if hours < 0 {
		return Employee{}, fmt.Errorf("hours cannot be negative: %d", hours)
	}
	rate, err := strconv.Atoi(trimmed[9:14])
	if err != nil {
		return Employee{}, fmt.Errorf("invalid hourly rate %q: %w", trimmed[9:14], err)
	}
	if rate < 0 {
		return Employee{}, fmt.Errorf("hourly rate cannot be negative: %d", rate)
	}
	return Employee{ID: id, Hours: hours, HourlyRate: rate}, nil
}

// Runner executes the payroll batch run against concrete input and output
// sinks, mirroring the COBOL program's EMP-FILE / PAY-FILE handling.
type Runner struct {
	Input  io.Reader // employees.txt, one record per line
	Output io.Writer // payroll.txt, one pay line per record
	Stdout io.Writer // grand-total line
}

// NewRunner builds a Runner reading from r, writing per-employee pay
// lines to out, and the grand total to stdout.
func NewRunner(r io.Reader, out, stdout io.Writer) *Runner {
	return &Runner{Input: r, Output: out, Stdout: stdout}
}

// Run processes every employee record until EOF, accumulates the grand
// total, writes one "id pay" line per record to Output, and finally
// writes "TOTAL <total>" to Stdout.
//
// It aborts on the first malformed record with a wrapped error that
// includes the line number. MIGRATION_NOTE: the original COBOL program
// had no handling for malformed records at all; failing loudly is the
// safe default — review whether skipping bad lines is preferred.
func (r *Runner) Run(ctx context.Context) (int, error) {
	scanner := bufio.NewScanner(r.Input)
	w := bufio.NewWriter(r.Output)
	defer w.Flush()

	total := 0
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		select {
		case <-ctx.Done():
			return total, fmt.Errorf("payroll run cancelled at line %d: %w", lineNum, ctx.Err())
		default:
		}

		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue // ignore blank lines between records
		}
		emp, err := parseRecord(line)
		if err != nil {
			return total, fmt.Errorf("line %d: %w", lineNum, err)
		}
		pay := computePay(emp.Hours, emp.HourlyRate)
		total += pay
		if _, err := fmt.Fprintf(w, "%s %s\n", emp.ID, FormatCents(pay, 7)); err != nil {
			return total, fmt.Errorf("write pay line %d: %w", lineNum, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return total, fmt.Errorf("read employees: %w", err)
	}
	if _, err := fmt.Fprintf(r.Stdout, "TOTAL %s\n", FormatCents(total, 9)); err != nil {
		return total, fmt.Errorf("write total: %w", err)
	}
	return total, nil
}

// Default input/output file names used by the original program.
const (
	DefaultInputFile  = "employees.txt"
	DefaultOutputFile = "payroll.txt"
)

// Main is the batch entry point, factored out so it can be driven by
// cmd/payroll/main.go (and tested) rather than living in package main.
func Main(ctx context.Context, args []string, stdout io.Writer) error {
	inPath, outPath := DefaultInputFile, DefaultOutputFile
	if len(args) > 0 {
		inPath = args[0]
	}
	if len(args) > 1 {
		outPath = args[1]
	}

	f, err := os.Open(inPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", inPath, err)
	}
	defer f.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outPath, err)
	}
	defer out.Close()

	_, err = NewRunner(f, out, stdout).Run(ctx)
	return err
}
