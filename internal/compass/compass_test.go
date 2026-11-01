package compass

import (
	"errors"
	"slices"
	"testing"

	"github.com/projectbluefin/chairlift/internal/flatpak"
)

type fakeHost struct {
	arch          string
	user, system  bool
	remotes       []string
	listErr       error
	remoteErr     error
	installErr    error
	uninstallErr  map[bool]error
	installs      int
	uninstalledIn []bool
}

func (h *fakeHost) apps(present bool) ([]flatpak.Application, error) {
	if h.listErr != nil {
		return nil, h.listErr
	}
	apps := []flatpak.Application{{ApplicationID: "org.example.Other"}}
	if present {
		apps = append(apps, flatpak.Application{ApplicationID: AppID})
	}
	return apps, nil
}

func installFake(t *testing.T, h *fakeHost) {
	t.Helper()
	old := []any{listUser, listSystem, remotes, install, uninstall, goarch}
	t.Cleanup(func() {
		listUser = old[0].(func() ([]flatpak.Application, error))
		listSystem = old[1].(func() ([]flatpak.Application, error))
		remotes = old[2].(func() ([]string, error))
		install = old[3].(func() error)
		uninstall = old[4].(func(string, bool) error)
		goarch = old[5].(string)
	})
	goarch = "amd64"
	if h.arch != "" {
		goarch = h.arch
	}
	listUser = func() ([]flatpak.Application, error) { return h.apps(h.user) }
	listSystem = func() ([]flatpak.Application, error) { return h.apps(h.system) }
	remotes = func() ([]string, error) { return h.remotes, h.remoteErr }
	install = func() error { h.installs++; return h.installErr }
	uninstall = func(id string, user bool) error {
		if id != AppID {
			t.Fatalf("uninstall asked for %q", id)
		}
		h.uninstalledIn = append(h.uninstalledIn, user)
		return h.uninstallErr[user]
	}
}

func TestStatusReportsEachScopeAndTheRemote(t *testing.T) {
	h := &fakeHost{user: true, remotes: []string{"flathub", Remote}}
	installFake(t, h)
	state, err := Status()
	if err != nil {
		t.Fatal(err)
	}
	if want := (State{User: true, RemoteConfigured: true, Supported: true}); state != want {
		t.Fatalf("Status() = %+v, want %+v", state, want)
	}
}

func TestStatusFailsWhenAnyReadFails(t *testing.T) {
	for name, h := range map[string]*fakeHost{
		"list":    {listErr: errors.New("no installations")},
		"remotes": {remoteErr: errors.New("no remotes")},
	} {
		t.Run(name, func(t *testing.T) {
			installFake(t, h)
			if _, err := Status(); err == nil {
				t.Fatal("Status() succeeded on a failed read")
			}
			if _, err := Enable(); err == nil || h.installs != 0 {
				t.Fatalf("Enable() = %v with %d installs after a failed read", err, h.installs)
			}
			if _, err := Disable(); err == nil || len(h.uninstalledIn) != 0 {
				t.Fatalf("Disable() = %v, removed %v after a failed read", err, h.uninstalledIn)
			}
		})
	}
}

func TestEnableInstallsOnlyWhenAbsentFromBothScopes(t *testing.T) {
	cases := map[string]struct {
		host        fakeHost
		wantInstall int
		wantErr     error
	}{
		"absent":              {host: fakeHost{remotes: []string{Remote}}, wantInstall: 1},
		"user copy":           {host: fakeHost{user: true, remotes: []string{Remote}}},
		"system copy":         {host: fakeHost{system: true, remotes: []string{Remote}}},
		"no remote":           {host: fakeHost{remotes: []string{"flathub"}}, wantErr: ErrRemoteMissing},
		"installed no remote": {host: fakeHost{system: true}},
		"arm64":               {host: fakeHost{arch: "arm64", remotes: []string{Remote}}, wantErr: ErrUnsupported},
		"arm64 installed":     {host: fakeHost{arch: "arm64", system: true, remotes: []string{Remote}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := tc.host
			installFake(t, &h)
			ran, err := Enable()
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Enable() error = %v, want %v", err, tc.wantErr)
			}
			if h.installs != tc.wantInstall || ran != (tc.wantInstall == 1) {
				t.Fatalf("Enable() ran=%v with %d installs, want %d", ran, h.installs, tc.wantInstall)
			}
		})
	}
}

func TestEnableReportsInstallFailure(t *testing.T) {
	h := &fakeHost{remotes: []string{Remote}, installErr: errors.New("auth dismissed")}
	installFake(t, h)
	if ran, err := Enable(); err == nil || ran {
		t.Fatalf("Enable() = %v, %v; want a failed, unchanged install", ran, err)
	}
}

func TestDisableRemovesEveryObservedScopeAndOnlyThose(t *testing.T) {
	cases := map[string]struct {
		host fakeHost
		want []bool
	}{
		"absent": {host: fakeHost{}},
		"user":   {host: fakeHost{user: true}, want: []bool{true}},
		"system": {host: fakeHost{system: true}, want: []bool{false}},
		"both":   {host: fakeHost{user: true, system: true}, want: []bool{true, false}},
		"arm64":  {host: fakeHost{arch: "arm64", system: true}, want: []bool{false}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := tc.host
			installFake(t, &h)
			ran, err := Disable()
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(h.uninstalledIn, tc.want) || ran != (len(tc.want) > 0) {
				t.Fatalf("Disable() ran=%v removed from user=%v, want %v", ran, h.uninstalledIn, tc.want)
			}
		})
	}
}

func TestDisableAttemptsBothScopesWhenOneFails(t *testing.T) {
	h := &fakeHost{user: true, system: true, uninstallErr: map[bool]error{true: errors.New("busy")}}
	installFake(t, h)
	_, err := Disable()
	if err == nil {
		t.Fatal("Disable() hid a failed user-scope removal")
	}
	if !slices.Equal(h.uninstalledIn, []bool{true, false}) {
		t.Fatalf("Disable() removed from user=%v, want both scopes attempted", h.uninstalledIn)
	}
}
