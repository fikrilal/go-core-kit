// Command bench measures response latencies for a running API: it registers an
// account, signs in, and reports percentiles per route. It is the harness behind
// docs/engineering/performance-baseline.md, kept in the repository so the numbers
// can be reproduced rather than trusted.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"
)

func main() {
	base := flag.String("base", "http://127.0.0.1:8080", "API base URL")
	requests := flag.Int("requests", 200, "requests per route")
	flag.Parse()

	client := &http.Client{Timeout: 10 * time.Second}
	token, err := signIn(client, *base)
	if err != nil {
		fmt.Fprintln(os.Stderr, "bench:", err)
		os.Exit(1)
	}

	routes := []struct {
		name  string
		path  string
		token bool
	}{
		{"GET /health", "/health", false},
		{"GET /v1/me", "/v1/me", true},
		{"GET /v1/me/sessions", "/v1/me/sessions", true},
	}
	for _, route := range routes {
		latencies := make([]time.Duration, 0, *requests)
		for i := 0; i < *requests; i++ {
			start := time.Now()
			status, err := get(client, *base+route.path, tokenOrEmpty(route.token, token))
			if err != nil {
				fmt.Fprintf(os.Stderr, "bench: %s: %v\n", route.name, err)
				os.Exit(1)
			}
			if status != http.StatusOK {
				fmt.Fprintf(os.Stderr, "bench: %s: status %d\n", route.name, status)
				os.Exit(1)
			}
			latencies = append(latencies, time.Since(start))
		}
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		fmt.Printf("%-22s n=%d  p50=%-9s p95=%-9s p99=%-9s max=%s\n",
			route.name, len(latencies),
			percentile(latencies, 0.50), percentile(latencies, 0.95),
			percentile(latencies, 0.99), latencies[len(latencies)-1])
	}
}

func tokenOrEmpty(needsToken bool, token string) string {
	if needsToken {
		return token
	}
	return ""
}

// signIn registers an account and returns its access token.
func signIn(client *http.Client, base string) (string, error) {
	email := fmt.Sprintf("bench-%d@example.com", time.Now().UnixNano())
	body, err := json.Marshal(map[string]string{
		"email": email, "password": "bench-password-1", "deviceId": "bench-device",
	})
	if err != nil {
		return "", fmt.Errorf("encode registration: %w", err)
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/v1/auth/password/register", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build registration: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("register: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("register: status %d", resp.StatusCode)
	}
	var decoded struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return "", fmt.Errorf("decode registration: %w", err)
	}
	if decoded.Data.AccessToken == "" {
		return "", fmt.Errorf("registration returned no access token")
	}
	return decoded.Data.AccessToken, nil
}

// get issues one request and returns its status.
func get(client *http.Client, url, token string) (int, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, nil
}

// percentile returns the latency at the given quantile of a sorted slice.
func percentile(sorted []time.Duration, quantile float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	index := int(float64(len(sorted)-1) * quantile)
	return sorted[index]
}
