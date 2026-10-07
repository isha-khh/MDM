package domain

import "testing"

func TestCanPerformRentalAction(t *testing.T) {
	cases := []struct {
		action                 RentalAction
		admin, custodian, want bool
	}{
		// admin or custodian
		{RentalApprove, true, false, true},
		{RentalApprove, false, true, true},
		{RentalApprove, false, false, false}, // an ordinary requester
		{RentalReject, false, true, true},
		{RentalReject, false, false, false},
		{RentalVerifyReturn, false, true, true},
		{RentalVerifyReturn, false, false, false},
		{RentalDecideExtension, false, true, true},
		{RentalDecideExtension, false, false, false},
		// admin only: a custodian alone is not enough
		{RentalActivate, true, false, true},
		{RentalActivate, false, true, false},
		{RentalActivate, false, false, false},
		{RentalDelete, true, false, true},
		{RentalDelete, false, true, false},
		{RentalDelete, false, false, false},
		// unknown actions are denied, even for admins
		{RentalAction("something-new"), true, true, false},
	}
	for _, c := range cases {
		if got := CanPerformRentalAction(c.action, c.admin, c.custodian); got != c.want {
			t.Errorf("%s admin=%v custodian=%v: got %v, want %v", c.action, c.admin, c.custodian, got, c.want)
		}
	}
}
