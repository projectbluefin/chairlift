package pageview

import (
	"github.com/projectbluefin/chairlift/internal/avatar"
)

// Profile Picture section text.
const (
	AvatarGroupTitle       = "Profile Picture"
	AvatarGroupDescription = "Shown on the login and lock screens"
	AvatarChooseLabel      = "Choose…"
	AvatarPickerTitle      = "Choose a Profile Picture"
	AvatarApplyLabel       = "Apply"
	AvatarPickerPrompt     = "Pick a dinosaur to preview it"
)

// AvatarCurrentRow describes the page row for the picture the account
// already has. applied is the catalog entry this session set, if any: the
// account stores a picture, not which dinosaur it was, so after a restart
// the row can only say whether a picture exists.
func AvatarCurrentRow(hasPicture bool, applied *avatar.Avatar) Row {
	switch {
	case applied != nil:
		return AvatarEntryRow(*applied)
	case hasPicture:
		return Row{Title: "Current picture", Subtitle: "Choose a dinosaur to replace it"}
	default:
		return Row{Title: "No picture set", Subtitle: "Choose a dinosaur from Project Bluefin's artwork"}
	}
}

// AvatarEntryRow is one catalog entry: the character's name and its species.
func AvatarEntryRow(entry avatar.Avatar) Row {
	return Row{Title: entry.CommonName, Subtitle: entry.Species}
}

// AvatarPreviewFailed is the chooser's banner when the artwork could not be
// downloaded or decoded. Nothing was applied, so it says so.
func AvatarPreviewFailed(entry avatar.Avatar) string {
	return "Could not download " + entry.CommonName + ". Your picture was not changed; check your connection and pick again."
}

// AvatarApplyFailed is the chooser's banner when the downloaded artwork
// could not be set on the account.
func AvatarApplyFailed(entry avatar.Avatar) string {
	return "Could not set " + entry.CommonName + " as your picture. Your picture was not changed; press Apply to try again."
}

// AvatarApplied is the toast after a dispatch returned without error. Only
// the bus route changes the running session; the face-file fallback is read
// at the next sign-in, and a dry run changed nothing at all.
func AvatarApplied(route avatar.Route, entry avatar.Avatar) string {
	switch route {
	case avatar.RouteBusctl:
		return entry.CommonName + " is now your profile picture"
	case avatar.RouteFaceFile:
		return entry.CommonName + " will be your profile picture after you next sign in"
	default:
		return "Dry run: " + entry.CommonName + " was previewed but not applied"
	}
}
