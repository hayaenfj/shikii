package systemone

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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
		"http://user:s3cr3t@host:8080/v1":                        "http://xxxxx@host:8080/v1",
		"https://s3cr3t@api.example.test/v1":                     "https://xxxxx@api.example.test/v1", // a token alone before the @
		"http://user:s3cr3t/token@127.0.0.1:8080":                "http://xxxxx@127.0.0.1:8080",       // a raw slash in the token: the parser reads another host
		"http://user:123/s3cr3t@127.0.0.1:8080/v1":               "http://xxxxx@127.0.0.1:8080/v1",
		"http://user:s3cr3t?token@127.0.0.1:8080":                "http://xxxxx@127.0.0.1:8080",
		"http://user:s3cr3t@127.0.0.1:80a6":                      "http://xxxxx@127.0.0.1:80a6",
		"http://user:s3cr3t@[::1":                                "http://xxxxx@[::1",
		"user:s3cr3t@127.0.0.1:8086":                             "xxxxx@127.0.0.1:8086",
		"user:s3cr3t@127.0.0.1:8086/cb?next=http://other.test/x": "xxxxx@127.0.0.1:8086/cb?next=http://other.test/x", // no scheme, and "://" later
		"http://host:8080/v1":                                    "http://host:8080/v1",
		"localhost:8086":                                         "localhost:8086",
		"":                                                       "",
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
		"http://tok:token-s3cr3t/x@127.0.0.1:80", // the user name is the start of the token
		"http://user:pa\"ss-s3cr3t@127.0.0.1:1",  // a quote in the token: the parser quotes the URL with an escape
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

// A scrubbed error still unwraps to its cause, also when it is not an error of the URL package.
func TestScrubErrorKeepsTheCause(t *testing.T) {
	err := ScrubError(fmt.Errorf("get http://u:secret-token@h/x: %w", context.DeadlineExceeded), "http://u:secret-token@h")
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "secret-token") || !strings.Contains(err.Error(), "http://xxxxx@h") {
		t.Errorf("scrubbed error: %v", err)
	}
	if plain := errors.New("nothing to hide"); ScrubError(plain, "http://h:1") != plain {
		t.Errorf("an error for a URL without credentials is returned as it is")
	}
}

// The HTTP client repeats the URL in its own percent-encoding (%2f as %2F, %65 as e, %3D as =) and decodes the user name, so no
// spelling of the credentials may be looked for and replaced: the text is written anew, without the client's copy of the URL.
// Nothing listens at the address, so every call fails on this machine.
func TestAskErrorHidesCredentialsInEverySpelling(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	host := strings.TrimPrefix(closed.URL, "http://")
	closed.Close()
	for raw, pieces := range map[string][]string{
		"http://sk-AbC%2FdEf%2B123%3D@" + host: {"sk-AbC", "dEf", "123="},
		"http://sk-abc%2fdef@" + host:          {"sk-abc", "def@"},
		"http://tok%65n-s3cr3t@" + host:        {"token-s3cr3t", "tok%65n", "s3cr3t"},
		"http://user:p%40ss-w0rd@" + host:      {"user:", "ss-w0rd"},
		"http://key%40s3cr3t-token:x@" + host:  {"s3cr3t", "key"}, // a token as the user name, with an encoded @
		"http://to%22ken-s3cr3t:x@" + host:     {"s3cr3t", "to\\\"ken", "to%22"},
		"http://tok:token-s3cr3t%2Fx@" + host:  {"s3cr3t", "token"},
	} {
		_, err := New(raw, "", "m").Ask(context.Background(), "state", map[string]Question{"q": {Type: "noul", Instructions: "?"}})
		if err == nil {
			t.Errorf("%q: answered", raw)
			continue
		}
		for _, piece := range pieces {
			if strings.Contains(err.Error(), piece) {
				t.Errorf("the error of a decision call carries %q of the credentials: %v", piece, err)
			}
		}
		if !strings.Contains(err.Error(), "xxxxx@"+host) || !strings.Contains(err.Error(), "connection refused") {
			t.Errorf("the error still names the host and the cause: %v", err)
		}
	}
}

// Credentials of one or two characters, or an empty pair, leave the words of the message whole (replacing them in the text
// broke every word that held the same characters), and a URL without credentials keeps its error as it is.
func TestAskErrorKeepsItsWordsWholeForShortOrEmptyCredentials(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	host := strings.TrimPrefix(closed.URL, "http://")
	closed.Close()
	for _, raw := range []string{"http://:@" + host, "http://a@" + host, "http://t:@" + host, "http://@" + host, "http://" + host} {
		unchanged := raw == "http://@"+host || raw == "http://"+host // nothing stands before an @: the client's own words stay
		_, err := New(raw, "", "m").Ask(context.Background(), "state", map[string]Question{"q": {Type: "noul", Instructions: "?"}})
		if err == nil {
			t.Errorf("%q: answered", raw)
			continue
		}
		text := err.Error()
		if !strings.Contains(text, "dial tcp "+host+": connect: connection refused") {
			t.Errorf("%q: the cause is no longer readable: %v", raw, err)
		}
		if n := strings.Count(text, "xxxxx"); n > 1 || unchanged && n != 0 {
			t.Errorf("%q: the message is broken up or rewritten (%d replacements): %v", raw, n, err)
		}
	}
}

// refusingClient never leaves the machine: its dial fails at once and names the host the transport asked for, the way the real
// dialer does ("dial tcp: lookup <host>: no such host").
func refusingClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, _ := net.SplitHostPort(addr)
			return nil, &net.OpError{Op: "dial", Net: network, Err: fmt.Errorf("lookup %s: no such host", host)}
		},
	}}
}

func refusedAsk(t *testing.T, raw string) error {
	t.Helper()
	c := New(raw, "", "m")
	c.HTTP = refusingClient()
	_, err := c.Ask(context.Background(), "state", map[string]Question{"q": {Type: "noul", Instructions: "?"}})
	if err == nil {
		t.Fatalf("%q: answered", raw)
	}
	return err
}

// Credentials with a raw "/" or "?" end early for the URL package: what stands before it is taken for the host (and is looked
// up, in lower case or in punycode), and the rest, with the @, for the path. The error of a decision call hides them all the
// same: it is written from the whole URL as it was given, and a cause that names another host than the URL's is left out.
func TestAskErrorHidesCredentialsThatEndEarlyForTheClient(t *testing.T) {
	for _, raw := range []string{
		"http://sk-abc/def-s3cr3t-token@127.0.0.1:8080",
		"http://user:123/s3cr3t-token@127.0.0.1:8080",
		"http://sk-abc/def-s3cr3t-token@127.0.0.1:8080/v1/systemone",
		"http://tökén-s3cr3t?x@127.0.0.1:8080",
		"http://TOKEN-S3CR3T?x@127.0.0.1:8080",
		"user:s3cr3t-token@127.0.0.1:8086/cb?next=http://other.test/x",
	} {
		err := refusedAsk(t, raw)
		low := strings.ToLower(err.Error())
		for _, piece := range []string{"s3cr3t", "sk-abc", "user:", "xn--"} {
			if strings.Contains(low, piece) {
				t.Errorf("%q: the error of a decision call carries %q: %v", raw, piece, err)
			}
		}
		if !strings.Contains(err.Error(), "xxxxx@127.0.0.1:808") {
			t.Errorf("%q: the error names the URL without its credentials: %v", raw, err)
		}
	}
}

// The cause is still quoted when it names the URL's own host, also when a user name is a word of that host; the original error
// stays reachable. A cause that names a piece of the credentials is left out.
func TestAskErrorKeepsACauseThatNamesTheURLsOwnHost(t *testing.T) {
	err := refusedAsk(t, "http://api:s3cr3t-token@api.example.test")
	if !strings.Contains(err.Error(), "lookup api.example.test: no such host") || strings.Contains(err.Error(), "s3cr3t") {
		t.Errorf("the cause about the URL's own host is kept: %v", err)
	}
	var op *net.OpError
	if !errors.As(err, &op) {
		t.Errorf("errors.As no longer reaches the cause: %v", err)
	}
	raw := "http://user:s3cr3t-token@h.example.test"
	named := ScrubError(&url.Error{Op: "Post", URL: raw, Err: errors.New("proxy said: bad token S3CR3T-TOKEN")}, raw)
	if strings.Contains(strings.ToLower(named.Error()), "s3cr3t") || !strings.Contains(named.Error(), "http://xxxxx@h.example.test") {
		t.Errorf("a cause that names a piece of the credentials is left out: %v", named)
	}
}
