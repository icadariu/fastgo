package main

import (
	"io"
	"os"
	"testing"
)

func TestHandleVersionFlag(t *testing.T) {
	tests := []struct {
		name string
		args []string // os.Args, including the program name at index 0
		want bool
	}{
		{"long flag", []string{"fastgo", "--version"}, true},
		{"short flag", []string{"fastgo", "-version"}, true},
		{"flag among other args", []string{"fastgo", "-parallel", "4", "--version"}, true},
		{"no flag", []string{"fastgo"}, false},
		{"unrelated args only", []string{"fastgo", "-parallel", "4", "-progress"}, false},
		{"substring is not a match", []string{"fastgo", "--versions"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origArgs := os.Args
			defer func() { os.Args = origArgs }()
			os.Args = tt.args

			// Swallow any stdout so the version line doesn't pollute test output.
			got := captureStdout(t, func() bool { return HandleVersionFlag() })
			if got.ret != tt.want {
				t.Errorf("HandleVersionFlag() = %v, want %v", got.ret, tt.want)
			}
		})
	}
}

func TestHandleVersionFlagOutput(t *testing.T) {
	origArgs := os.Args
	origVersion, origCommit, origBuildTime := version, commit, buildTime
	defer func() {
		os.Args = origArgs
		version, commit, buildTime = origVersion, origCommit, origBuildTime
	}()

	os.Args = []string{"fastgo", "--version"}
	version, commit, buildTime = "1.2.3", "abc1234", "10-06-26_12:00"

	out := captureStdout(t, func() bool { return HandleVersionFlag() })
	if !out.ret {
		t.Fatalf("HandleVersionFlag() = false, want true")
	}

	want := "1.2.3 (built 10-06-26_12:00, commit abc1234)\n"
	if out.text != want {
		t.Errorf("output = %q, want %q", out.text, want)
	}
}

type capturedRun struct {
	ret  bool
	text string
}

// captureStdout redirects os.Stdout for the duration of fn and returns fn's
// boolean result alongside everything it printed.
func captureStdout(t *testing.T, fn func() bool) capturedRun {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	ret := fn()

	w.Close()
	os.Stdout = orig

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	r.Close()

	return capturedRun{ret: ret, text: string(data)}
}
