package pageview

// Powerwash is the pure-presentation side of the Powerwash detail view: the
// single named place a user returns to a previous system version or performs
// an explicitly scoped reset. It is reached deliberately from Maintenance, so the
// strings here describe a destination, not a routine control.
//
// The bootc rollback row and the two reset rows all live in their own
// pageview functions (BootcRollbackRow, PowerwashRow, FactoryResetRow); this
// file only
// supplies the container-level copy that is new to Powerwash.

// RecoveryPageSubtitle is the Powerwash detail page's heading description. It
// names both jobs — return to a previous version, or reset — without promising
// either more than the backend can do.
func RecoveryPageSubtitle() string {
	return "Return to a previous system version, or reset this machine to how it shipped."
}

// RecoveryEntrySubtitle is the subtitle on the Maintenance page's Powerwash entry.
// It points the user at the detail view rather than exposing a control here,
// so the routine Maintenance page never reaches a reset.
func RecoveryEntrySubtitle() string {
	return "Return to a previous system version or reset this machine."
}
