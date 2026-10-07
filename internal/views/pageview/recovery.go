package pageview

// Powerwash is the pure-presentation side of the Powerwash detail view: the
// single named place a user returns to a previous version or performs an
// explicitly scoped reset. It is reached deliberately from Maintenance, so the
// strings here describe a destination, not a routine control.
//
// The rollback row and the two reset rows all live in their own pageview
// functions (BootcRollbackRow, PowerwashRow, FactoryResetRow); this file only
// supplies the entry copy that is new to Powerwash.

// RecoveryEntrySubtitle is the subtitle on the Maintenance page's Powerwash entry.
// It points the user at the detail view rather than exposing a control here,
// so the routine Maintenance page never reaches a reset.
func RecoveryEntrySubtitle() string {
	return "Go back to an earlier version or reset this computer."
}
