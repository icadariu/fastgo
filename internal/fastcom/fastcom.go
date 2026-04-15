package fastcom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

var (
	reScript = regexp.MustCompile(`<script[^>]+src="([^"]+)"`)
	reToken  = regexp.MustCompile(`token\s*:\s*"([^"]+)"`)
)

func newClient() *http.Client {
	tr := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     30 * time.Second,
	}

	return &http.Client{
		Transport: tr,
		Timeout:   60 * time.Second,
	}
}

func FetchAppToken(ctx context.Context) (string, error) {
	c := newClient()

	req, _ := http.NewRequestWithContext(ctx, "GET", "https://fast.com/", nil)
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	htmlBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	m := reScript.FindSubmatch(htmlBytes)
	if len(m) < 2 {
		return "", errors.New("fast.com script not found")
	}

	scriptURL := string(m[1])
	if strings.HasPrefix(scriptURL, "/") {
		scriptURL = "https://fast.com" + scriptURL
	}

	req2, _ := http.NewRequestWithContext(ctx, "GET", scriptURL, nil)
	resp2, err := c.Do(req2)
	if err != nil {
		return "", err
	}
	defer resp2.Body.Close()

	jsBytes, err := io.ReadAll(resp2.Body)
	if err != nil {
		return "", err
	}

	t := reToken.FindSubmatch(jsBytes)
	if len(t) < 2 {
		return "", errors.New("token not found in JS")
	}

	return string(t[1]), nil
}

type targetItem struct {
	URL string `json:"url"`
}

type targetsObjResp struct {
	Client struct {
		Ip string `json:"ip"`
	} `json:"client"`
	Targets []targetItem `json:"targets"`
}

func FetchTargets(ctx context.Context, token string, urlCount int) ([]string, string, error) {
	c := newClient()

	u := fmt.Sprintf(
		"https://api.fast.com/netflix/speedtest/v2?https=true&token=%s&urlCount=%d",
		token,
		urlCount,
	)

	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	resp, err := c.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	// Most common: object format with client + targets
	var obj targetsObjResp
	if err := json.Unmarshal(body, &obj); err == nil && len(obj.Targets) > 0 {
		urls := make([]string, 0, len(obj.Targets))
		for _, t := range obj.Targets {
			if t.URL != "" {
				urls = append(urls, t.URL)
			}
		}
		if len(urls) == 0 {
			return nil, obj.Client.Ip, errors.New("no targets returned (object format)")
		}
		return urls, obj.Client.Ip, nil
	}

	// Fallback: array format (older behavior)
	var arr []targetItem
	if err := json.Unmarshal(body, &arr); err == nil && len(arr) > 0 {
		urls := make([]string, 0, len(arr))
		for _, t := range arr {
			if t.URL != "" {
				urls = append(urls, t.URL)
			}
		}
		if len(urls) == 0 {
			return nil, "", errors.New("no targets returned (array format)")
		}
		return urls, "", nil
	}

	return nil, "", errors.New("unexpected speedtest response shape from api.fast.com")
}

type Result struct {
	Mbps     float64
	Bytes    int64
	Duration time.Duration
}

func MeasureDownload(
	ctx context.Context,
	urls []string,
	parallel int,
	duration time.Duration,
	showProgress bool,
	tick time.Duration,
) (Result, error) {

	if parallel < 1 {
		parallel = 1
	}
	if tick <= 0 {
		tick = 500 * time.Millisecond
	}

	c := newClient()
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var totalBytes int64
	start := time.Now()

	// Progress printer
	doneProg := make(chan struct{})
	if showProgress {
		go func() {
			t := time.NewTicker(tick)
			defer t.Stop()
			var lastBytes int64
			lastTime := time.Now()

			for {
				select {
				case <-doneProg:
					fmt.Print("\r")
					return
				case <-t.C:
					now := time.Now()
					b := atomic.LoadInt64(&totalBytes)

					deltaB := b - lastBytes
					deltaT := now.Sub(lastTime).Seconds()
					instMbps := 0.0
					if deltaT > 0 {
						instMbps = (float64(deltaB) * 8) / deltaT / 1_000_000
					}

					elapsed := now.Sub(start).Seconds()
					avgMbps := 0.0
					if elapsed > 0 {
						avgMbps = (float64(b) * 8) / elapsed / 1_000_000
					}

					fmt.Printf("\r  DL avg: %.2f Mbps | inst: %.2f Mbps", avgMbps, instMbps)

					lastBytes = b
					lastTime = now
				}
			}
		}()
	}

	errCh := make(chan error, parallel)

	for i := 0; i < parallel; i++ {
		go func(worker int) {
			idx := worker % len(urls)
			for {
				select {
				case <-ctx.Done():
					errCh <- nil
					return
				default:
				}

				url := urls[idx]
				idx = (idx + 1) % len(urls)

				req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
				resp, err := c.Do(req)
				if err != nil {
					if ctx.Err() != nil {
						continue
					}
					errCh <- err
					return
				}

				n, _ := io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				atomic.AddInt64(&totalBytes, n)
			}
		}(i)
	}

	for i := 0; i < parallel; i++ {
		if err := <-errCh; err != nil {
			if showProgress {
				close(doneProg)
			}
			return Result{}, err
		}
	}

	if showProgress {
		close(doneProg)
		fmt.Println()
	}

	elapsed := time.Since(start)
	mbps := (float64(totalBytes) * 8) / elapsed.Seconds() / 1_000_000

	return Result{
		Mbps:     mbps,
		Bytes:    totalBytes,
		Duration: elapsed,
	}, nil
}

type countReader struct {
	n *int64
}

func (r *countReader) Read(p []byte) (int, error) {
	// Fill with zeros (fast; content doesn't matter for throughput)
	for i := range p {
		p[i] = 0
	}
	atomic.AddInt64(r.n, int64(len(p)))
	return len(p), nil
}

func MeasureUpload(
	ctx context.Context,
	urls []string,
	parallel int,
	duration time.Duration,
	showProgress bool,
	tick time.Duration,
) (Result, error) {

	if parallel < 1 {
		parallel = 1
	}
	if tick <= 0 {
		tick = 500 * time.Millisecond
	}

	c := newClient()
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	var totalBytes int64
	start := time.Now()

	// Progress printer
	doneProg := make(chan struct{})
	if showProgress {
		go func() {
			t := time.NewTicker(tick)
			defer t.Stop()
			var lastBytes int64
			lastTime := time.Now()

			for {
				select {
				case <-doneProg:
					fmt.Print("\r")
					return
				case <-t.C:
					now := time.Now()
					b := atomic.LoadInt64(&totalBytes)

					deltaB := b - lastBytes
					deltaT := now.Sub(lastTime).Seconds()
					instMbps := 0.0
					if deltaT > 0 {
						instMbps = (float64(deltaB) * 8) / deltaT / 1_000_000
					}

					elapsed := now.Sub(start).Seconds()
					avgMbps := 0.0
					if elapsed > 0 {
						avgMbps = (float64(b) * 8) / elapsed / 1_000_000
					}

					fmt.Printf("\r  UL avg: %.2f Mbps | inst: %.2f Mbps", avgMbps, instMbps)

					lastBytes = b
					lastTime = now
				}
			}
		}()
	}

	errCh := make(chan error, parallel)

	for i := 0; i < parallel; i++ {
		go func(worker int) {
			idx := worker % len(urls)

			for {
				select {
				case <-ctx.Done():
					errCh <- nil
					return
				default:
				}

				url := urls[idx]
				idx = (idx + 1) % len(urls)

				// Generate bytes and count them
				var workerBytes int64
				r := &countReader{n: &workerBytes}

				req, _ := http.NewRequestWithContext(ctx, "POST", url, io.LimitReader(r, 1<<62))
				req.Header.Set("Content-Type", "application/octet-stream")

				resp, err := c.Do(req)
				if err != nil {
					if ctx.Err() != nil {
						continue
					}
					errCh <- err
					return
				}

				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusForbidden {
					errCh <- fmt.Errorf("upload rejected by target (HTTP %d)", resp.StatusCode)
					return
				}

				atomic.AddInt64(&totalBytes, workerBytes)
			}
		}(i)
	}

	for i := 0; i < parallel; i++ {
		if err := <-errCh; err != nil {
			if showProgress {
				close(doneProg)
			}
			return Result{}, err
		}
	}

	if showProgress {
		close(doneProg)
		fmt.Println()
	}

	elapsed := time.Since(start)
	mbps := (float64(totalBytes) * 8) / elapsed.Seconds() / 1_000_000

	return Result{
		Mbps:     mbps,
		Bytes:    totalBytes,
		Duration: elapsed,
	}, nil
}
