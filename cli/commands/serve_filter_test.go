package commands

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

// A single over-long line used to stop the reader for good: bufio.Scanner
// reports ErrTooLong, Scan returns false, and every later line — including the
// app's ready marker — is lost while the server keeps running.
func TestOneEnormousLineDoesNotSilenceTheRest(t *testing.T) {
	huge := strings.Repeat("x", maxLogLine*3)
	input := "first\n" + huge + "\nsecond\nthird\n"
	r := bufio.NewReaderSize(strings.NewReader(input), 64*1024)

	var got []string
	for {
		line, err := readLine(r, maxLogLine)
		if line != "" {
			got = append(got, line)
		}
		if err != nil {
			break
		}
	}

	if len(got) != 4 {
		t.Fatalf("read %d lines, want 4: %v", len(got), summarise(got))
	}
	if got[0] != "first" || got[2] != "second" || got[3] != "third" {
		t.Errorf("lines after the long one were lost: %v", summarise(got))
	}
	if len(got[1]) != maxLogLine {
		t.Errorf("the long line came back as %d bytes, want it capped at %d", len(got[1]), maxLogLine)
	}
}

// Ordinary output is unaffected, including a final line with no newline.
func TestReadLineHandlesOrdinaryOutput(t *testing.T) {
	r := bufio.NewReaderSize(strings.NewReader("alpha\nbeta\ngamma"), 4096)
	var got []string
	for {
		line, err := readLine(r, maxLogLine)
		if line != "" {
			got = append(got, line)
		}
		if err != nil {
			break
		}
	}
	if strings.Join(got, ",") != "alpha,beta,gamma" {
		t.Errorf("got %v", got)
	}
}

// A marker the CLI cannot decode must still reach the terminal. Swallowing it
// leaves a running server looking hung.
func TestUndecodableMarkerIsShownRatherThanDropped(t *testing.T) {
	var out bytes.Buffer
	f := &airFilter{out: &out}

	f.showReady("__NIMBUS_READY__ {not valid json")

	if out.Len() == 0 {
		t.Fatal("an undecodable ready marker produced no output at all")
	}
	if !strings.Contains(out.String(), "__NIMBUS_READY__") {
		t.Errorf("the raw marker was not shown: %q", out.String())
	}
}

func summarise(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if len(l) > 20 {
			out[i] = l[:20] + "…"
			continue
		}
		out[i] = l
	}
	return out
}
