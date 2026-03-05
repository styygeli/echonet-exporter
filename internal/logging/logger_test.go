package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoggerSplitsStdoutAndStderrByLevel(t *testing.T) {
	var out bytes.Buffer
	var errOut bytes.Buffer

	l := NewWithWriters("test", &out, &errOut)
	if e := SetLevel("debug"); e != nil {
		t.Fatalf("set level: %v", e)
	}
	defer func() {
		_ = SetLevel("info")
	}()

	l.Debugf("debug message")
	l.Infof("info message")
	l.Warnf("warn message")
	l.Errorf("error message")

	stdout := out.String()
	stderr := errOut.String()

	if !strings.Contains(stdout, "debug message") || !strings.Contains(stdout, "info message") {
		t.Fatalf("stdout missing debug/info logs: %q", stdout)
	}
	if strings.Contains(stdout, "warn message") || strings.Contains(stdout, "error message") {
		t.Fatalf("stdout should not contain warn/error logs: %q", stdout)
	}

	if !strings.Contains(stderr, "warn message") || !strings.Contains(stderr, "error message") {
		t.Fatalf("stderr missing warn/error logs: %q", stderr)
	}
	if strings.Contains(stderr, "debug message") || strings.Contains(stderr, "info message") {
		t.Fatalf("stderr should not contain debug/info logs: %q", stderr)
	}
}
