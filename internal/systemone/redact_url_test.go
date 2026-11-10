package systemone

import (
	"context"
	"errors"
	"fmt"
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
		"http://user:s3cr3t@host:8080/v1":          "http://xxxxx@host:8080/v1",
		"https://s3cr3t@api.example.test/v1":       "https://xxxxx@api.example.test/v1", // a token alone before the @
		"http://user:s3cr3t/token@127.0.0.1:8080":  "http://xxxxx@127.0.0.1:8080",       // a raw slash in the token: the parser reads another host
		"http://user:123/s3cr3t@127.0.0.1:8080/v1": "http://xxxxx@127.0.0.1:8080/v1",
		"http://user:s3cr3t?token@127.0.0.1:8080":  "http://xxxxx@127.0.0.1:8080",
		"http://user:s3cr3t@127.0.0.1:80a6":        "http://xxxxx@127.0.0.1:80a6",
		"http://user:s3cr3t@[::1":                  "http://xxxxx@[::1",
		"user:s3cr3t@127.0.0.1:8086":               "xxxxx@127.0.0.1:8086",
		"http://host:8080/v1":                      "http://host:8080/v1",
		"localhost:8086":                           "localhost:8086",
		"":                                         "",
	} {
		if got := RedactURL(raw); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

// The errors of a decision call hide the credentials too: the parser's and the client's errors repeat the URL, or a piece of
// it (the "port" after a colon, the "scheme" before one), in their own words. No request leaves the machine here.
func TestAskErrorHidesTheCredentialsOfTheModelURL(t *testing.T) {
	for _, raw := range []string{
		"http://user:s3cr3t-token@127.0.0.1:80a6",
		"user:s3cr3t-token@127.0.0.1:8086",
		"http://user:s3cr3t-token/more@127.0.0.1:80a6",
		"http://s3cr3t-token@[::1",
	} {
		_, err := New(raw, "", "m").Ask(context.Background(), "state", map[string]Question{"q": {Type: "noul", Instructions: "?"}})
		if err == nil {
			t.Errorf("%q: answered", raw)
			continue
		}
		if strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("the error of a decision call carries the credentials of the URL: %v", err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := WaitReady(ctx, "http://user:s3cr3t/token@127.0.0.1:80a6", time.Hour, nil); err == nil || strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("the wait's error carries a piece of the credentials: %v", err)
	}
}

// A scrubbed error still unwraps to its cause.
func TestScrubErrorKeepsTheCause(t *testing.T) {
	err := ScrubError(fmt.Errorf("get http://u:secret-token@h/x: %w", context.DeadlineExceeded), "http://u:secret-token@h")
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "secret-token") {
		t.Errorf("scrubbed error: %v", err)
	}
	if plain := errors.New("nothing to hide"); ScrubError(plain, "http://h:1") != plain {
		t.Errorf("an error without credentials is returned as it is")
	}
}
