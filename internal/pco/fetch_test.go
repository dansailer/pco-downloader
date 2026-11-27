package pco

import (
	"net"
	"testing"
)

func TestRefusesPrivateFileURL(t *testing.T) {
	c := New("a", "b")
	blocked := []string{
		"http://127.0.0.1/secret",
		"https://169.254.169.254/latest/meta-data",
		"https://user:pass@cdn.example/a.mp3",
		"https://10.1.2.3/file",
		"http://metadata.google.internal/computeMetadata/v1/",
		"https://cdn.example/a.mp3",
	}
	// The last entry is a public HTTPS URL and must be accepted.
	for _, raw := range blocked[:len(blocked)-1] {
		if err := c.validateFetchURL(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := c.validateFetchURL(blocked[len(blocked)-1]); err != nil {
		t.Fatal(err)
	}
	if c.allowIP("api.planningcenteronline.com", net.ParseIP("127.0.0.1")) {
		t.Fatal("public API host accepted a loopback address")
	}
	if c.allowIP("api.planningcenteronline.com", net.ParseIP("100.64.0.1")) {
		t.Fatal("public API host accepted a carrier-grade NAT address")
	}
	if !c.allowIP("cdn.example", net.ParseIP("8.8.8.8")) {
		t.Fatal("public address was refused")
	}

	c.BaseURL = "http://127.0.0.1:9"
	if err := c.validateFetchURL("http://127.0.0.1:9/file/1"); err != nil {
		t.Fatal(err)
	}
	if !c.allowIP("127.0.0.1", net.ParseIP("127.0.0.1")) {
		t.Fatal("local API host refused itself")
	}
	if c.allowIP("10.1.2.3", net.ParseIP("10.1.2.3")) {
		t.Fatal("a private host other than the API host was accepted")
	}
}

func TestResolveStaysOnAPIOrigin(t *testing.T) {
	c := New("a", "b")
	got, err := c.resolve("/services/v2/songs/1")
	if err != nil || got != "https://api.planningcenteronline.com/services/v2/songs/1" {
		t.Fatalf("got %s err %v", got, err)
	}
	blocked := []string{
		"http://api.planningcenteronline.com/services/v2/songs/1",
		"https://user:pass@api.planningcenteronline.com/services/v2/songs/1",
		"https://api.planningcenteronline.com:8443/services/v2/songs/1",
		"https://evil.example/services/v2/songs/1",
	}
	for _, ref := range blocked {
		if _, err := c.resolve(ref); err == nil {
			t.Errorf("accepted %s", ref)
		}
	}

	c.BaseURL = "http://127.0.0.1:9"
	got, err = c.resolve("/services/v2/songs/1")
	if err != nil || got != "http://127.0.0.1:9/services/v2/songs/1" {
		t.Fatalf("local got %s err %v", got, err)
	}
	if _, err := c.resolve("http://127.0.0.1/services/v2/songs/1"); err == nil {
		t.Fatal("accepted a different local port")
	}
	if _, err := c.resolve("https://127.0.0.1:9/services/v2/songs/1"); err == nil {
		t.Fatal("accepted a scheme change")
	}
}

func TestErrorDetailOmitsBody(t *testing.T) {
	got := errorDetail(500, []byte("please ignore, token=supersecret"))
	if got != "HTTP 500" {
		t.Fatal(got)
	}
	got = errorDetail(422, []byte(`{"errors":[{"detail":"Name taken"}]}`))
	if got != "HTTP 422: Name taken" {
		t.Fatal(got)
	}
}

func TestProxyHostAllowed(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8888")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("ALL_PROXY", "")
	t.Setenv("http_proxy", "")
	t.Setenv("https_proxy", "")
	t.Setenv("all_proxy", "")
	if !proxyHost("127.0.0.1") {
		t.Fatal("configured proxy was refused")
	}
	if proxyHost("169.254.169.254") {
		t.Fatal("metadata address was treated as the proxy")
	}
}
