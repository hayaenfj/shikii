package main

import "testing"

// audit D (0.31.0, second round), finding A-6 (INVARIANT 13): three tests taken in from the first round (audit_0_31_a_codex_test.go)
// run init and hook install in a temporary HOME and left the model's address as the package has it (the real model server's port).
// The same test functions run here, and every request they send to that address is counted (modelPortRequests; the audit counted
// with a server put in place of hookLocalURL, which no longer tells the real address from the closed one TestMain sets). The control
// uses freshMachine.
func TestAuditD_A6_ImportedTestsLookAtTheModelPort(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"TestAuditACodexApprovalLineComesBackAfterThePersonRemovedIt", TestAuditACodexApprovalLineComesBackAfterThePersonRemovedIt},
		{"TestAuditACodexReadOnlyConfigAndThePreview", TestAuditACodexReadOnlyConfigAndThePreview},
		{"TestAuditACodexApprovalLineCommentedOutByThePerson", TestAuditACodexApprovalLineCommentedOutByThePerson},
		{"control, TestInitInstallsTheHooksForTheFirstProjectOnAMachine", TestInitInstallsTheHooksForTheFirstProjectOnAMachine},
	} {
		n := modelPortRequests(t, func() { t.Run(c.name, c.run) })
		t.Logf("%s: requests to the model's address while it ran: %d", c.name, n)
		if n > 0 {
			t.Errorf("%s asks the model's address as the package has it (%s): %d requests", c.name, shippedModelURL, n)
		}
	}
}
