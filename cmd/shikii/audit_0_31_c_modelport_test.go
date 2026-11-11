package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// modelPortRequests runs fn and returns how many requests it sent to the model's address as the package ships it (shippedModelURL:
// the port of the real model server on the machine that runs the tests). Such a request is counted and answered here with an
// error, so counting never reaches that port. Every client of the package uses the default transport.
func modelPortRequests(t *testing.T, fn func()) int {
	t.Helper()
	shipped, err := url.Parse(shippedModelURL)
	if err != nil || shipped.Host == "" {
		t.Fatalf("the address the package ships: %q %v", shippedModelURL, err)
	}
	var asked atomic.Int64
	old := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == shipped.Host {
			asked.Add(1)
			return nil, errors.New("a test asked the model's real address (counted, not sent)")
		}
		return old.RoundTrip(r)
	})
	defer func() { http.DefaultTransport = old }()
	fn()
	return int(asked.Load())
}

// TestMain closes the model's address and the PATH lookup for every test. This fails when either is left as the package ships it.
// What it checks does not depend on the machine: sh is on every PATH, so only a replaced lookup does not find it.
func TestTheTestsStartWithTheModelPortClosedAndNoPathLookup(t *testing.T) {
	if shippedModelURL != "http://127.0.0.1:8080" {
		t.Errorf("the package ships the real model's port as the model's address (init writes it into the registrations): %s", shippedModelURL)
	}
	if hookLocalURL == shippedModelURL {
		t.Errorf("INVARIANT 13: a test starts with the model's real address (%s): TestMain closes it, and a test that changes it puts it back", hookLocalURL)
	}
	if found, err := lookPath("sh"); err == nil {
		t.Errorf("INVARIANT 13: a test looks programs up on this machine's PATH (found %s): TestMain replaces lookPath, and a test that changes it puts it back", found)
	}
	// A test that does nothing but set a temporary HOME and install the hooks: nothing is asked of the model's real address, no
	// llama-server is found on this machine, and nothing is started for it.
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	spawned = nil
	var out string
	asked := modelPortRequests(t, func() { out = capture(t, func() error { return hookInstall([]string{"--yes"}, true) }) })
	if asked != 0 {
		t.Errorf("INVARIANT 13: hook install --yes in a temporary HOME asked the model's real address %d times", asked)
	}
	if !strings.Contains(out, "no llama-server on PATH") || strings.Contains(out, "starting in the background") || len(spawned) != 0 {
		t.Errorf("hook install --yes in a temporary HOME looked for llama-server on this machine's PATH (started: %v):\n%s", spawned, lastLines(out, 3))
	}
}

func lastLines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// audit C (0.31.0, second round), finding A-8 (INVARIANT 13): tests of the candidate that ran init or hook install --yes in a fresh
// HOME without closing the model port asked the model's real address for /health (17 of the tests below did). They run here as
// they are, and every request to that address is counted. The audit counted with a server put in place of hookLocalURL; since
// TestMain closes the address for every test, the requests to the address the package ships are counted instead (modelPortRequests).
func TestAuditCShippedTestsLookAtTheModelPort(t *testing.T) {
	if shippedModelURL != "http://127.0.0.1:8080" {
		t.Fatalf("precondition: the package ships the real model's port as the model's address: %s", shippedModelURL)
	}
	looked := []string{}
	for _, c := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"TestAuditACodexApprovalLineComesBackAfterThePersonRemovedIt", TestAuditACodexApprovalLineComesBackAfterThePersonRemovedIt},
		{"TestAuditACodexReadOnlyConfigAndThePreview", TestAuditACodexReadOnlyConfigAndThePreview},
		{"TestAuditACodexApprovalLineCommentedOutByThePerson", TestAuditACodexApprovalLineCommentedOutByThePerson},
		{"TestHookInstallIsIdempotentAndKeepsOtherHooks", TestHookInstallIsIdempotentAndKeepsOtherHooks},
		{"TestInitPreparesTheProjectAndTheAgentInOneGo", TestInitPreparesTheProjectAndTheAgentInOneGo},
		{"TestInitSeedsTheWikiWithSharedKnowledgeWithoutOverwriting", TestInitSeedsTheWikiWithSharedKnowledgeWithoutOverwriting},
		{"TestInitInAMovedFolderReconnectsIt", TestInitInAMovedFolderReconnectsIt},
		{"TestInitNeverBreaksWhatPeopleWrote", TestInitNeverBreaksWhatPeopleWrote},
		{"TestCLIHandoff", TestCLIHandoff},
		{"TestCLIHandoffRefusesTooLongStdin", TestCLIHandoffRefusesTooLongStdin},
		{"TestLayoutDefaultsLiveUnderTheStore", TestLayoutDefaultsLiveUnderTheStore},
		{"TestIngestErrorSaysWhatIsWrong", TestIngestErrorSaysWhatIsWrong},
		{"TestCaptureErrorsSayWhatIsWrong", TestCaptureErrorsSayWhatIsWrong},
		{"TestLongSourceIsCapturedWhole", TestLongSourceIsCapturedWhole},
		{"TestRulesReachAgentsWithoutTheAgentsSection", TestRulesReachAgentsWithoutTheAgentsSection},
		{"TestPendingSourcesAreListedByTheirHead", TestPendingSourcesAreListedByTheirHead},
		{"TestDoctorSaysWhenTheRegisteredBinaryIsGone", TestDoctorSaysWhenTheRegisteredBinaryIsGone},
		{"TestDoctorNamesAFixForCodexThatWorksForThatBlock", TestDoctorNamesAFixForCodexThatWorksForThatBlock},
		{"TestDoctorSaysAHandWrittenShikiiHookWasNotChecked", TestDoctorSaysAHandWrittenShikiiHookWasNotChecked},
		// controls: the two tests round 1 named, which close the port themselves
		{"TestInitInstallsTheHooksForTheFirstProjectOnAMachine", TestInitInstallsTheHooksForTheFirstProjectOnAMachine},
		{"TestInitAndHookInstallLetCodexRunShikiisTools", TestInitAndHookInstallLetCodexRunShikiisTools},
	} {
		passed := false
		n := modelPortRequests(t, func() { passed = t.Run(c.name, c.run) })
		t.Logf("%-62s passed=%v requests to the model port: %d", c.name, passed, n)
		if n > 0 {
			looked = append(looked, c.name)
		}
	}
	if len(looked) > 0 {
		t.Errorf("INVARIANT 13: %d tests of the candidate ask the model port for /health (%s): %s", len(looked), shippedModelURL, strings.Join(looked, ", "))
	}
}
