package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// What a person and the page are shown about the model URLs never carries the credentials in them: the lines of `shikii doctor`
// (the URL and the error of the probe, which repeats the URL in the HTTP client's own encoding) and the endpoint in /v1/status.
func TestDoctorAndStatusHideTheCredentialsOfTheModelURLs(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	host := strings.TrimPrefix(closed.URL, "http://")
	closed.Close() // nothing listens there: the probes fail on this machine
	local, hosted := "http://user:l0cal-s3cr3t@"+host, "http://sk-AbC%2FdEf%2B123@"+host
	secrets := []string{"l0cal-s3cr3t", "sk-AbC", "dEf", "user:"}

	dir := t.TempDir()
	writeSpec(t, dir, "true")
	out := capture(t, func() error {
		return cmdDoctor([]string{"--specs", dir, "--store", filepath.Join(dir, ".shikii"), "--local", local, "--hosted", hosted})
	})
	if !strings.Contains(out, "xxxxx@"+host) || !strings.Contains(out, "hosted") || !strings.Contains(out, "unreachable") {
		t.Fatalf("doctor names both model URLs without their credentials:\n%s", out)
	}
	for _, secret := range secrets {
		if strings.Contains(out, secret) {
			t.Errorf("doctor shows %q of the credentials:\n%s", secret, out)
		}
	}

	var ef endpointFlags
	ef.model, ef.local = "m", local
	srv := httptest.NewServer(newServer(serveOpts{specsDir: dir, storeDir: filepath.Join(dir, ".shikii"), ef: ef}))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/v1/status")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "xxxxx@"+host) {
		t.Fatalf("status names the local model without its credentials: %d %s", resp.StatusCode, body)
	}
	for _, secret := range secrets {
		if strings.Contains(string(body), secret) {
			t.Errorf("status shows %q of the credentials: %s", secret, body)
		}
	}
}
