// Package oracle carries the account-state matrix the email jobs are asserted
// against, so the states and their outcomes live in one place: a new skip is a row
// here rather than a new test in two suites. It holds data, not logic, and exists for
// tests — the same trade internal/platform/dbtest makes.
package oracle

// AccountState names the state an account can be in when a job touches it.
type AccountState string

// The states the email jobs distinguish.
const (
	Missing   AccountState = "missing"
	Deleted   AccountState = "deleted"
	Suspended AccountState = "suspended"
	Active    AccountState = "active"
)

// EmailExpectation is what a handler must report for a state, and whether it may
// send anything at all.
type EmailExpectation struct {
	// Outcome is the value the handler reports; the feature's own constants are the
	// vocabulary, and a rename shows up here as a failing assertion.
	Outcome string
	// Sends is true when the state is one the handler mails.
	Sends bool
}

// Verification is the matrix for the verification email job.
var Verification = map[AccountState]EmailExpectation{
	Missing:   {Outcome: "user_not_found"},
	Deleted:   {Outcome: "already_deleted"},
	Suspended: {Outcome: "sent", Sends: true},
	Active:    {Outcome: "sent", Sends: true},
}

// PasswordReset is the matrix for the password reset email job. A suspended account
// may still reset its password; a deleted one is gone.
var PasswordReset = map[AccountState]EmailExpectation{
	Missing:   {Outcome: "user_not_found"},
	Deleted:   {Outcome: "already_deleted"},
	Suspended: {Outcome: "sent", Sends: true},
	Active:    {Outcome: "sent", Sends: true},
}

// States returns the matrix's keys in a stable order, so a test's subtests read the
// same way on every run.
func States() []AccountState {
	return []AccountState{Missing, Deleted, Suspended, Active}
}
