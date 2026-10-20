package payroll

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputePay(t *testing.T) {
	tests := []struct {
		name       string
		hours      int
		hourlyRate int
		want       int
	}{
		{"zero hours", 0, 1000, 0},
		{"straight time 20h at $10", 20, 1000, 20000},
		{"exactly 40h", 40, 1000, 40000},
		{"overtime 45h at $10 -> 475.00", 45, 1000, 47500},
		{"overtime 41h at $10 -> 415.00", 41, 1000, 41500},
		{"rate in cents", 10, 101, 1010},
		{"fractional cent rounds half up", 1, 101, 101}, // 1*101/100 = 1.01
		{"fractional cent rounds up at half", 1, 105, 105},
		{"overtime fractional rounds half up", 41, 501, 40*501/100 + (501*3)/(2*100)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computePay(tc.hours, tc.hourlyRate)
			if got != tc.want {
				t.Errorf("computePay(%d, %d) = %d, want %d", tc.hours, tc.hourlyRate, got, tc.want)
			}
		})
	}
}

func TestRoundHalfUp(t *testing.T) {
	tests := []struct {
		num, den, want int
	}{
		{0, 100, 0},
		{50, 100, 1},
		{49, 100, 0},
		{150, 100, 2},
		{99, 100, 1},
		{199, 200, 1},
		{200, 200, 1},
	}
	for _, tc := range tests {
		if got := roundHalfUp(tc.num, tc.den); got != tc.want {
			t.Errorf("roundHalfUp(%d, %d) = %d, want %d", tc.num, tc.den, got, tc.want)
		}
	}
}

func TestFormatCents(t *testing.T) {
	tests := []struct {
		name  string
		cents int
		width int
		want  string
	}{
		{"zero", 0, 7, "   0.00"},
		{"simple", 123456, 7, " 1234.56"},
		{"width 9", 1234567, 9, "  12345.67"},
		{"blank suppression", 5, 7, "   0.05"},
		{"three digits", 999, 7, "   9.99"},
		{"full width", 1234567, 7, "12345.67"},
		{"overflow truncates high-order", 12345678901, 7, "6789.01"},
		{"negative gets leading dash", -100, 7, "-   1.00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatCents(tc.cents, tc.width)
			if got != tc.want {
				t.Errorf("FormatCents(%d, %d) = %q, want %q", tc.cents, tc.width, got, tc.want)
			}
		})
	}
}

func TestParseRecord(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		want    Employee
		wantErr bool
	}{
		{"valid", "0001 020 01000", Employee{ID: "0001", Hours: 20, HourlyRate: 1000}, false},
		{"trailing whitespace ok", "0001 020 01000   ", Employee{ID: "0001", Hours: 20, HourlyRate: 1000}, false},
		{"max hours", "0002 999 99999", Employee{ID: "0002", Hours: 999, HourlyRate: 99999}, false},
		{"leading zeros kept in id", "0007 008 00100", Employee{ID: "0007", Hours: 8, HourlyRate: 100}, false},
		{"too short", "0001 020 0100", Employee{}, true},
		{"empty", "", Employee{}, true},
		{"bad separator pos 4", "0001020 01000", Employee{}, true},
		{"bad separator pos 8", "0001 020x01000", Employee{}, true},
		{"non-numeric hours", "0001 abc 01000", Employee{}, true},
		{"non-numeric rate", "0001 020 0a000", Employee{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseRecord(tc.line)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseRecord(%q) expected error, got %v", tc.line, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseRecord(%q) unexpected error: %v", tc.line, err)
			}
			if got != tc.want {
				t.Errorf("parseRecord(%q) = %+v, want %+v", tc.line, got, tc.want)
			}
		})
	}
}

func TestRunnerRun(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantOutput   string
		wantStdout   string
		wantTotal    int
		wantErr      bool
		errSubstring string
	}{
		{
			name:       "empty input",
			input:      "",
			wantOutput: "",
			wantStdout: "TOTAL         0.00\n",
			wantTotal:  0,
		},
		{
			name:       "single straight-time employee",
			input:      "0001 020 01000\n",
			wantOutput: "0001   200.00\n",
			wantStdout: "TOTAL       200.00\n",
			wantTotal:  20000,
		},
		{
			name:       "overtime employee",
			input:      "0002 045 01000\n",
			wantOutput: "0002   475.00\n",
			wantStdout: "TOTAL       475.00\n",
			wantTotal:  47500,
		},
		{
			name:       "zero hours",
			input:      "0003 000 01000\n",
			wantOutput: "0003     0.00\n",
			wantStdout: "TOTAL         0.00\n",
			wantTotal:  0,
		},
		{
			name:       "multiple employees in order plus blank lines",
			input:      "0001 020 01000\n\n0002 045 01000\n",
			wantOutput: "0001   200.00\n0002   475.00\n",
			wantStdout: "TOTAL       675.00\n",
			wantTotal:  67500,
		},
		{
			name:         "malformed record aborts",
			input:        "0001 020 01000\nbad\n",
			wantOutput:   "0001   200.00\n",
			wantTotal:    20000,
			wantErr:      true,
			errSubstring: "line 2",
		},
		{
			name:         "negative-looking invalid hours",
			input:        "0001 -20 01000\n",
			wantErr:      true,
			errSubstring: "line 1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var outBuf, stdoutBuf bytes.Buffer
			r := NewRunner(strings.NewReader(tc.input), &outBuf, &stdoutBuf)
			total, err := r.Run(context.Background())

			if tc.wantErr {
				if err == nil {
					t.Fatalf("Run() expected error, got nil")
				}
				if tc.errSubstring != "" && !strings.Contains(err.Error(), tc.errSubstring) {
					t.Errorf("Run() error = %q, want substring %q", err.Error(), tc.errSubstring)
				}
			} else if err != nil {
				t.Fatalf("Run() unexpected error: %v", err)
			}

			if total != tc.wantTotal {
				t.Errorf("Run() total = %d, want %d", total, tc.wantTotal)
			}
			if outBuf.String() != tc.wantOutput {
				t.Errorf("payroll output = %q, want %q", outBuf.String(), tc.wantOutput)
			}
			if stdoutBuf.String() != tc.wantStdout {
				t.Errorf("stdout = %q, want %q", stdoutBuf.String(), tc.wantStdout)
			}
		})
	}
}

func TestRunnerRunRoundingAccumulatesRoundedValues(t *testing.T) {
	// 1h at $1.01 -> 101 cents; per-employee line shows 1.01; total equals sum of rounded pays.
	input := "0001 001 00101\n0002 001 00101\n"
	var outBuf, stdoutBuf bytes.Buffer
	r := NewRunner(strings.NewReader(input), &outBuf, &stdoutBuf)
	total, err := r.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if total != 202 {
		t.Errorf("total = %d, want 202", total)
	}
	if outBuf.String() != "0001   1.01\n0002   1.01\n" {
		t.Errorf("output = %q", outBuf.String())
	}
	if stdoutBuf.String() != "TOTAL         2.02\n" {
		t.Errorf("stdout = %q", stdoutBuf.String())
	}
}

func TestRunnerRunContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var outBuf, stdoutBuf bytes.Buffer
	r := NewRunner(strings.NewReader("0001 020 01000\n"), &outBuf, &stdoutBuf)
	if _, err := r.Run(ctx); err == nil {
		t.Fatal("Run() expected cancellation error, got nil")
	}
}

func TestMain(t *testing.T) {
	dir := t.TempDir()
	inPath := filepath.Join(dir, "employees.txt")
	outPath := filepath.Join(dir, "payroll.txt")
	if err := os.WriteFile(inPath, []byte("0001 020 01000\n0002 045 01000\n0003 000 01000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := Main(context.Background(), []string{inPath, outPath}, &stdout); err != nil {
		t.Fatalf("Main() error: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	want := "0001   200.00\n0002   475.00\n0003     0.00\n"
	if string(got) != want {
		t.Errorf("payroll.txt = %q, want %q", string(got), want)
	}
	if stdout.String() != "TOTAL       675.00\n" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "TOTAL       675.00\n")
	}
}

func TestMainMissingInput(t *testing.T) {
	dir := t.TempDir()
	var stdout bytes.Buffer
	if err := Main(context.Background(), []string{filepath.Join(dir, "nope.txt"), filepath.Join(dir, "out.txt")}, &stdout); err == nil {
		t.Fatal("Main() expected error for missing input file, got nil")
	}
}