package app

import "testing"

func TestRoleQueues(t *testing.T) {
	cases := []struct {
		role, status string
		want         bool
	}{{"requester", "submitted", false}, {"requester", "needs_info", true}, {"requester", "awaiting_confirmation", true}, {"admin", "submitted", true}, {"admin", "unassigned", true}, {"admin", "assigned", false}, {"technician", "assigned", true}, {"technician", "in_progress", true}, {"technician", "awaiting_confirmation", false}, {"admin", "closed", false}}
	for _, c := range cases {
		if actionable(c.role, c.status) != c.want {
			t.Fatalf("incorrect actionable status: %s %s", c.role, c.status)
		}
	}
	for _, v := range []string{"closed", "rejected", "cancelled"} {
		if !finished(v) {
			t.Fatal("terminal status not finished")
		}
	}
	if finished("unassigned") {
		t.Fatal("reopened order finished")
	}
	if _, _, err := queueFilter("requester", "invalid"); err == nil {
		t.Fatal("invalid queue accepted")
	}
	q, args, err := queueFilter("requester", "action")
	if err != nil || q == "" || len(args) != 2 {
		t.Fatal("queue clause invalid")
	}
}
