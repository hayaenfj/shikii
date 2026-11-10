package systemone

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The error for a model URL that cannot become a request names the URL without the credentials in it, also when the URL does
// not parse (RedactURL used to return such a URL as it was written) or was written without its scheme.
func TestWaitReadyErrorHidesTheCredentialsOfAURLThatDoesNotParse(t *testing.T) {
	for _, raw := range []string{"http://user:s3cr3t-token@127.0.0.1:80a6", "http://user:s3cr3t-token@[::1", "http://user:s3cr3t-token@host/%zz", "user:s3cr3t-token@127.0.0.1:8086"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := WaitReady(ctx, raw, time.Hour, nil)
		cancel()
		if err == nil {
			t.Errorf("%q: reported ready", raw)
			continue
		}
		if strings.Contains(err.Error(), "s3cr3t-token") {
			t.Errorf("the error carries the password of the URL: %v", err)
		}
	}
}

func TestRedactURL(t *testing.T) {
	for raw, want := range map[string]string{
		"http://user:s3cr3t@host:8080/v1":   "http://user:xxxxx@host:8080/v1", // a URL that parses keeps the user name, as before
		"http://host:8080/v1/x@y":           "http://host:8080/v1/x@y",
		"http://user:s3cr3t@127.0.0.1:80a6": "http://xxxxx@127.0.0.1:80a6",
		"http://user:s3cr3t@[::1":           "http://xxxxx@[::1",
		"user:s3cr3t@127.0.0.1:8086":        "xxxxx@127.0.0.1:8086",
		"a:b@c:d@host:80a6/path@x":          "xxxxx@host:80a6/path@x",
		"localhost:8086":                    "localhost:8086",
		"":                                  "",
	} {
		if got := RedactURL(raw); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", raw, got, want)
		}
	}
}
