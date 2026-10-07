package domain

// RentalAction is a state-changing operation on a rental batch that only
// certain people may perform.
type RentalAction string

const (
	RentalApprove         RentalAction = "approve"       // pending -> approved
	RentalReject          RentalAction = "reject"        // pending -> rejected
	RentalActivate        RentalAction = "activate"      // approved -> active (hand the devices out)
	RentalVerifyReturn    RentalAction = "verify-return" // pending_return -> returned
	RentalDecideExtension RentalAction = "extension"     // approve/reject a renewal request
	RentalDelete          RentalAction = "delete"        // remove the whole batch
)

// CanPerformRentalAction is the rule behind who may do what, mirroring what
// the web UI offers: approving, rejecting, verifying a return and deciding a
// renewal belong to an admin or the custodian of an asset in the batch;
// handing devices out and deleting a batch are admin only. Borrower-side
// actions (submit-return, daily-report, extend) are checked separately against
// the batch's borrower.
func CanPerformRentalAction(action RentalAction, isAdmin, isBatchCustodian bool) bool {
	switch action {
	case RentalActivate, RentalDelete:
		return isAdmin
	case RentalApprove, RentalReject, RentalVerifyReturn, RentalDecideExtension:
		return isAdmin || isBatchCustodian
	default:
		return false
	}
}
