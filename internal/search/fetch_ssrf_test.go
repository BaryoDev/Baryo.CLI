// SPDX-License-Identifier: MIT

package search

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestBlockedAddr(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", "::1",
		"10.0.0.1", "172.16.5.5", "192.168.1.1", "fc00::1", "fd12:3456::1",
		"169.254.169.254", "169.254.0.1", "fe80::1",
		"0.0.0.0", "::", "0.1.2.3",
		"224.0.0.1", "239.255.255.250", "ff02::1",
		"::ffff:127.0.0.1", "::ffff:169.254.169.254", "::ffff:10.0.0.1",
	}
	for _, s := range blocked {
		if blockedAddr(netip.MustParseAddr(s)) == "" {
			t.Errorf("%s should be blocked", s)
		}
	}
	allowed := []string{"93.184.216.34", "8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"}
	for _, s := range allowed {
		if why := blockedAddr(netip.MustParseAddr(s)); why != "" {
			t.Errorf("%s should be allowed, blocked as %s", s, why)
		}
	}
}

func TestFetchContentRefusesLoopbackServer(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<p>INTERNAL-METADATA token=abc</p>")
	}))
	defer internal.Close()

	out, err := FetchContent(context.Background(), internal.URL)
	if err == nil || strings.Contains(out, "INTERNAL-METADATA") {
		t.Fatalf("loopback fetch must be refused, got %q err=%v", out, err)
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("error should say the address was refused, got %v", err)
	}
}

func TestFetchContentRefusesLiteralMetadataAndIPv6(t *testing.T) {
	for _, u := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]:9/",
		"http://10.0.0.1/",
		"http://0.0.0.0:9/",
	} {
		if _, err := FetchContent(context.Background(), u); err == nil || !strings.Contains(err.Error(), "refusing") {
			t.Errorf("%s: want a refusal, got %v", u, err)
		}
	}
}

// The first hop is allowed through the test hook, then it redirects to a
// second loopback server. The guard must refuse the second hop.
func TestFetchRefusesRedirectToBlockedAddress(t *testing.T) {
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<p>INTERNAL-METADATA token=abc</p>")
	}))
	defer internal.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal.URL, http.StatusFound)
	}))
	defer redirect.Close()

	redirectAddr := strings.TrimPrefix(redirect.URL, "http://")
	old := fetchClient
	fetchClient = newFetchClient(func(hostport string) bool { return hostport == redirectAddr })
	defer func() { fetchClient = old }()

	out, err := FetchContent(context.Background(), redirect.URL)
	if err == nil || strings.Contains(out, "INTERNAL-METADATA") {
		t.Fatalf("redirect to loopback must be refused, got %q err=%v", out, err)
	}
	if !strings.Contains(err.Error(), "refusing") {
		t.Errorf("error should come from the dial guard, got %v", err)
	}
}

// With the hook allowing the server, the client still fetches normally, so the
// refusal above is the guard and not a broken client.
func TestFetchWorksWhenAddressAllowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<p>hello page</p>")
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "http://")
	old := fetchClient
	fetchClient = newFetchClient(func(hostport string) bool { return hostport == addr })
	defer func() { fetchClient = old }()

	out, err := FetchContent(context.Background(), srv.URL)
	if err != nil || !strings.Contains(out, "hello page") {
		t.Fatalf("got %q err=%v", out, err)
	}
}

func TestFetchContentHonoursCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	addr := strings.TrimPrefix(srv.URL, "http://")
	old := fetchClient
	fetchClient = newFetchClient(func(hostport string) bool { return hostport == addr })
	defer func() { fetchClient = old }()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := FetchContent(ctx, srv.URL)
	if err == nil {
		t.Fatal("want an error after cancel")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("cancel took %v", time.Since(start))
	}
}
