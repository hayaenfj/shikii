package systemone

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
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
		if !strings.Contains(err.Error(), "xxxxx@"+host) || !strings.Contains(err.Error(), "the connection was refused") {
			t.Errorf("the error still names the host and the kind of failure: %v", err)
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
		if unchanged && !strings.Contains(text, "dial tcp "+host+": connect: connection refused") || !unchanged && !strings.Contains(text, "the connection was refused") {
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

// For a URL with credentials nothing of the cause's own text is quoted: the kind of failure is named in fixed words, whatever
// the cause says (a looked-up name in punycode, a certificate error naming the host the client read out of the credentials, a
// proxy repeating the token). The original error stays reachable.
func TestScrubErrorNamesTheKindOfFailureInFixedWords(t *testing.T) {
	raw := "https://t\u00f6k\u00e9n-s3cr3t?x@127.0.0.1:8080"
	for want, cause := range map[string]error{
		"the request timed out":                       context.DeadlineExceeded,
		"the request was canceled":                    context.Canceled,
		"the host name could not be looked up":        &net.DNSError{Err: "no such host", Name: "xn--tkn-s3cr3t-c7a8m", IsNotFound: true},
		"the connection was refused":                  &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		"the connection could not be opened":          &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("lookup xn--tkn-s3cr3t-c7a8m: no such host")},
		"the TLS certificate could not be verified":   fmt.Errorf("tls: failed to verify certificate: %w", x509.HostnameError{Certificate: &x509.Certificate{DNSNames: []string{"example.com"}}, Host: "xn--tkn-s3cr3t-c7a8m"}),
		"the URL needs an http:// or https:// scheme": errors.New(`unsupported protocol scheme "tok-s3cr3t"`),
		"it could not be completed":                   errors.New("proxy said: bad token t\u00f6k\u00e9n-s3cr3t"),
	} {
		err := ScrubError(&url.Error{Op: "Post", URL: raw + "/v1/systemone", Err: cause}, raw)
		text := err.Error()
		if !strings.Contains(text, "Post https://xxxxx@127.0.0.1:8080: "+want) {
			t.Errorf("want %q, got %v", want, err)
		}
		if low := strings.ToLower(text); strings.Contains(low, "s3cr3t") || strings.Contains(low, "xn--") {
			t.Errorf("the error carries a piece of the credentials: %v", err)
		}
		if !errors.Is(err, cause) {
			t.Errorf("the cause is no longer reachable: %v", err)
		}
	}
	// the user name is a word of the host: still nothing of the cause is quoted, and the host is named once, in the URL
	err := refusedAsk(t, "http://api:s3cr3t-token@api.example.test")
	var op *net.OpError
	if !errors.As(err, &op) || !strings.Contains(err.Error(), "Post http://xxxxx@api.example.test") || !strings.Contains(err.Error(), "the connection could not be opened") {
		t.Errorf("the kind of failure and the host are named, and the cause is reachable: %v", err)
	}
}

// answering is a client whose every request gets the status and body, with the host the transport asked for put in the body, as a
// proxy's error page does.
type answering struct {
	status int
}

func (a answering) RoundTrip(req *http.Request) (*http.Response, error) {
	body := "bad gateway: cannot reach " + req.URL.Host
	return &http.Response{StatusCode: a.status, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
}

// An HTTP status error names the URL as it was given, without its credentials. When the credentials made the client talk to
// another host (a raw "/" in them), the response is left out: it may name that host, a piece of the credentials. A URL whose
// credentials the client reads as credentials keeps the response, which says what the server refused.
func TestAskStatusErrorHidesTheCredentialsOfTheModelURL(t *testing.T) {
	ask := func(raw string) string {
		c := New(raw, "", "m")
		c.HTTP = &http.Client{Transport: answering{status: 502}}
		_, err := c.Ask(context.Background(), "state", map[string]Question{"q": {Type: "noul", Instructions: "?"}})
		if err == nil {
			t.Fatalf("%q: answered", raw)
		}
		return err.Error()
	}
	for _, raw := range []string{"http://sk-abc/def-s3cr3t-token@127.0.0.1:8080", "http://user:8443/s3cr3t-token@127.0.0.1:8080"} {
		text := ask(raw)
		for _, piece := range []string{"sk-abc", "user", "8443", "s3cr3t"} {
			if strings.Contains(text, piece) {
				t.Errorf("%q: the status error carries %q of the credentials: %s", raw, piece, text)
			}
		}
		if !strings.Contains(text, "systemone http://xxxxx@127.0.0.1:8080: HTTP 502") {
			t.Errorf("%q: the status error names the URL without its credentials: %s", raw, text)
		}
	}
	if text := ask("http://user:s3cr3t-token@127.0.0.1:8080"); !strings.Contains(text, "systemone http://xxxxx@127.0.0.1:8080: HTTP 502: bad gateway: cannot reach 127.0.0.1:8080") || strings.Contains(text, "s3cr3t") {
		t.Errorf("credentials the client reads as credentials: the response is kept and the URL is shown without them: %s", text)
	}
	if text := ask("http://127.0.0.1:8080"); !strings.Contains(text, "systemone http://127.0.0.1:8080: HTTP 502: bad gateway: cannot reach 127.0.0.1:8080") {
		t.Errorf("a URL without credentials keeps its status error: %s", text)
	}
}
