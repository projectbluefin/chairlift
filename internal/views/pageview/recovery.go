package pageview

// Recovery is the pure-presentation side of the Recovery detail view: the
// single named place a user returns to a previous system version or performs
// an explicitly scoped reset. It is reached deliberately from System, so the
// strings here describe a destination, not a routine control.
//
// The bootc rollback row and the two reset rows all live in their own
// pageview functions (BootcRollbackRow, PowerwashRow, FactoryResetRow); this
// file only
// supplies the container-level copy that is new to Recovery.

// RecoveryPageSubtitle is the Recovery detail page's heading description. It
// names both jobs — return to a previous version, or reset — without promising
// either more than the backend can do.
func RecoveryPageSubtitle() string {
	return "Return to a previous system version, or reset this machine to how it shipped."
}

// RecoveryEntrySubtitle is the subtitle on the System page's Recovery entry.
// It points the user at the detail view rather than exposing a control here,
// so the routine System page never reaches a reset.
func RecoveryEntrySubtitle() string {
	return "Recovery — return to a previous system version or reset this machine. Only appears when there is something to return to or a reset is enabled."
}
