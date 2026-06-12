package fastcom

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReScript(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string // "" means no match expected
	}{
		{"relative src", `<html><script src="/app-abc.js"></script>`, "/app-abc.js"},
		{"absolute src", `<script defer src="https://cdn.fast.com/x.js">`, "https://cdn.fast.com/x.js"},
		{"no script", `<html><body>nothing here</body></html>`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := reScript.FindStringSubmatch(tt.html)
			if tt.want == "" {
				if m != nil {
					t.Fatalf("expected no match, got %v", m)
				}
				return
			}
			if len(m) < 2 || m[1] != tt.want {
				t.Fatalf("reScript got %v, want capture %q", m, tt.want)
			}
		})
	}
}

func TestReToken(t *testing.T) {
	tests := []struct {
		name string
		js   string
		want string
	}{
		{"simple", `var t = {token:"deadbeef"};`, "deadbeef"},
		{"spaced", `token : "spaced-token"`, "spaced-token"},
		{"absent", `var x = 1;`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := reToken.FindStringSubmatch(tt.js)
			if tt.want == "" {
				if m != nil {
					t.Fatalf("expected no match, got %v", m)
				}
				return
			}
			if len(m) < 2 || m[1] != tt.want {
				t.Fatalf("reToken got %v, want capture %q", m, tt.want)
			}
		})
	}
}

func TestCountReader(t *testing.T) {
	var n int64
	r := &countReader{n: &n}

	buf := make([]byte, 1024)
	// Pre-fill with non-zero to prove Read overwrites with zeros.
	for i := range buf {
		buf[i] = 0xFF
	}

	got, err := r.Read(buf)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if got != len(buf) {
		t.Fatalf("Read returned %d, want %d", got, len(buf))
	}
	if n != int64(len(buf)) {
		t.Fatalf("counter = %d, want %d", n, len(buf))
	}
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("byte %d = %#x, want 0", i, b)
		}
	}
}

func TestFetchAppToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			fmt.Fprint(w, `<html><head><script src="/app-xyz.js"></script></head></html>`)
		case "/app-xyz.js":
			fmt.Fprint(w, `(function(){ var c = {token:"tok-12345"}; })();`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	restore := overrideBaseURL(&fastBaseURL, srv.URL)
	defer restore()

	tok, err := FetchAppToken(context.Background())
	if err != nil {
		t.Fatalf("FetchAppToken error: %v", err)
	}
	if tok != "tok-12345" {
		t.Fatalf("token = %q, want %q", tok, "tok-12345")
	}
}

func TestFetchAppTokenAbsoluteScriptURL(t *testing.T) {
	// The script handler lives on a second server, referenced by an absolute
	// URL in the HTML — exercises the non-relative-join branch.
	script := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `token:"abs-token"`)
	}))
	defer script.Close()

	root := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<script src="%s/app.js"></script>`, script.URL)
	}))
	defer root.Close()

	restore := overrideBaseURL(&fastBaseURL, root.URL)
	defer restore()

	tok, err := FetchAppToken(context.Background())
	if err != nil {
		t.Fatalf("FetchAppToken error: %v", err)
	}
	if tok != "abs-token" {
		t.Fatalf("token = %q, want %q", tok, "abs-token")
	}
}

func TestFetchAppTokenErrors(t *testing.T) {
	tests := []struct {
		name    string
		root    string // body returned at "/"
		script  string // body returned at "/app.js"
		wantErr string
	}{
		{"no script tag", `<html>no script here</html>`, ``, "script not found"},
		{"token missing", `<script src="/app.js"></script>`, `var x = 1;`, "token not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/" {
					fmt.Fprint(w, tt.root)
					return
				}
				fmt.Fprint(w, tt.script)
			}))
			defer srv.Close()

			restore := overrideBaseURL(&fastBaseURL, srv.URL)
			defer restore()

			_, err := FetchAppToken(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestFetchTargets(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		wantURLs []string
		wantIP   string
		wantErr  string
	}{
		{
			name:     "object format",
			body:     `{"client":{"ip":"1.2.3.4"},"targets":[{"url":"https://a/x"},{"url":"https://b/y"}]}`,
			wantURLs: []string{"https://a/x", "https://b/y"},
			wantIP:   "1.2.3.4",
		},
		{
			name:     "array format",
			body:     `[{"url":"https://c/z"}]`,
			wantURLs: []string{"https://c/z"},
			wantIP:   "",
		},
		{
			name:    "empty targets",
			body:    `{"client":{"ip":"9.9.9.9"},"targets":[]}`,
			wantErr: "unexpected speedtest response shape",
		},
		{
			name:    "garbage shape",
			body:    `{"unexpected":true}`,
			wantErr: "unexpected speedtest response shape",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()

			restore := overrideBaseURL(&apiBaseURL, srv.URL)
			defer restore()

			urls, ip, err := FetchTargets(context.Background(), "token", 5)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ip != tt.wantIP {
				t.Errorf("ip = %q, want %q", ip, tt.wantIP)
			}
			if !equalStrings(urls, tt.wantURLs) {
				t.Errorf("urls = %v, want %v", urls, tt.wantURLs)
			}
		})
	}
}

func TestMeasureDownload(t *testing.T) {
	payload := make([]byte, 32*1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer srv.Close()

	res, err := MeasureDownload(context.Background(), []string{srv.URL}, 4, 150*time.Millisecond, false, 0)
	if err != nil {
		t.Fatalf("MeasureDownload error: %v", err)
	}
	if res.Bytes <= 0 {
		t.Errorf("Bytes = %d, want > 0", res.Bytes)
	}
	if res.Mbps <= 0 {
		t.Errorf("Mbps = %v, want > 0", res.Mbps)
	}
	if res.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", res.Duration)
	}
}

func TestMeasureDownloadParallelNormalized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 4096))
	}))
	defer srv.Close()

	// parallel < 1 must be normalized to 1, not panic or hang.
	res, err := MeasureDownload(context.Background(), []string{srv.URL}, 0, 100*time.Millisecond, false, 0)
	if err != nil {
		t.Fatalf("MeasureDownload error: %v", err)
	}
	if res.Bytes <= 0 {
		t.Errorf("Bytes = %d, want > 0", res.Bytes)
	}
}

func TestMeasureDownloadCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(make([]byte, 4096))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	done := make(chan struct{})
	go func() {
		_, err := MeasureDownload(ctx, []string{srv.URL}, 4, time.Second, false, 0)
		if err != nil {
			t.Errorf("expected nil error on cancelled context, got %v", err)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("MeasureDownload did not return promptly on a cancelled context")
	}
}

func TestMeasureUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The client streams an effectively unbounded body; read a bounded
		// chunk (as real upload targets do) and respond, rather than draining
		// forever.
		io.CopyN(io.Discard, r.Body, 1<<20)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	res, err := MeasureUpload(context.Background(), []string{srv.URL}, 4, 150*time.Millisecond, false, 0)
	if err != nil {
		t.Fatalf("MeasureUpload error: %v", err)
	}
	if res.Bytes <= 0 {
		t.Errorf("Bytes = %d, want > 0", res.Bytes)
	}
}

func TestMeasureUploadRejected(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Read a bounded chunk first so the client's body write
				// succeeds, then reject — mirrors a real target refusing uploads.
				io.CopyN(io.Discard, r.Body, 64*1024)
				w.WriteHeader(status)
			}))
			defer srv.Close()

			_, err := MeasureUpload(context.Background(), []string{srv.URL}, 2, time.Second, false, 0)
			if err == nil {
				t.Fatalf("expected error for HTTP %d, got nil", status)
			}
			want := fmt.Sprintf("upload rejected by target (HTTP %d)", status)
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want containing %q", err, want)
			}
		})
	}
}

// overrideBaseURL points a base-URL var at v and returns a restore func.
func overrideBaseURL(p *string, v string) func() {
	orig := *p
	*p = v
	return func() { *p = orig }
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
