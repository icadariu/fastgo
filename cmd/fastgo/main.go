package main

import (
  "context"
  "flag"
  "fmt"
  "os"
  "time"

  "example.com/fastgo/internal/fastcom"
)

func main() {
  if HandleVersionFlag() {
    return
  }
  urlCount := flag.Int("urlcount", 5, "number of Fast.com target URLs to request")
  parallel := flag.Int("parallel", 8, "number of parallel workers")
  duration := flag.Duration("duration", 12*time.Second, "duration of each test phase")
  timeout := flag.Duration("timeout", 120*time.Second, "overall timeout")
  progress := flag.Bool("progress", true, "show live progress output")
  tick := flag.Duration("tick", 500*time.Millisecond, "progress update interval")
  flag.Parse()

  ctx, cancel := context.WithTimeout(context.Background(), *timeout)
  defer cancel()

  fmt.Println("Fetching token...")
  token, err := fastcom.FetchAppToken(ctx)
  if err != nil {
    fmt.Fprintf(os.Stderr, "error fetching token: %v\n", err)
    os.Exit(1)
  }

  fmt.Println("Fetching targets...")
  urls, ip, err := fastcom.FetchTargets(ctx, token, *urlCount)
  if err != nil {
    fmt.Fprintf(os.Stderr, "error fetching targets: %v\n", err)
    os.Exit(1)
  }
  if ip != "" {
    fmt.Printf("Client IP: %s\n", ip)
  }

  fmt.Println("Measuring download...")
  dl, err := fastcom.MeasureDownload(ctx, urls, *parallel, *duration, *progress, *tick)
  if err != nil {
    fmt.Fprintf(os.Stderr, "download error: %v\n", err)
    os.Exit(1)
  }

  fmt.Println("Measuring upload...")
  ul, err := fastcom.MeasureUpload(ctx, urls, *parallel, *duration, *progress, *tick)
  if err != nil {
    fmt.Fprintf(os.Stderr, "upload error: %v\n", err)
    os.Exit(1)
  }

  fmt.Printf("\nResults:\n")
  fmt.Printf("  Download: %.2f Mbps\n", dl.Mbps)
  fmt.Printf("  Upload:   %.2f Mbps\n", ul.Mbps)
}
