package firefoxfetch

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type firefoxExecutableInformation struct {
	name string
	mode os.FileMode
	uid  uint32
	sys  any
}

func (information firefoxExecutableInformation) Name() string       { return information.name }
func (information firefoxExecutableInformation) Size() int64        { return 0 }
func (information firefoxExecutableInformation) Mode() os.FileMode  { return information.mode }
func (information firefoxExecutableInformation) ModTime() time.Time { return time.Time{} }
func (information firefoxExecutableInformation) IsDir() bool {
	return information.mode.IsDir()
}

func (information firefoxExecutableInformation) Sys() any {
	if information.sys != nil {
		return information.sys
	}

	return &syscall.Stat_t{Uid: information.uid}
}

type firefoxExecutableFixture struct {
	paths       map[string]firefoxExecutableInformation
	locations   map[string]string
	resolutions map[string]string
	accessError error
}

func newFirefoxExecutableFixture() *firefoxExecutableFixture {
	return &firefoxExecutableFixture{
		paths: map[string]firefoxExecutableInformation{
			"/":                                {name: "/", mode: os.ModeDir | 0o755},
			"/usr":                             {name: "usr", mode: os.ModeDir | 0o755},
			"/usr/bin":                         {name: "bin", mode: os.ModeDir | 0o755},
			"/usr/bin/firefox-esr":             {name: "firefox-esr", mode: os.ModeSymlink | 0o777},
			"/usr/lib":                         {name: "lib", mode: os.ModeDir | 0o755},
			"/usr/lib/firefox-esr":             {name: "firefox-esr", mode: os.ModeDir | 0o755},
			"/usr/lib/firefox-esr/firefox-esr": {name: "firefox-esr", mode: 0o755},
		},
		locations: map[string]string{
			"firefox-esr": "/usr/bin/firefox-esr",
		},
		resolutions: map[string]string{
			"/usr/bin/firefox-esr": "/usr/lib/firefox-esr/firefox-esr",
		},
	}
}

func (fixture *firefoxExecutableFixture) filesystem() firefoxExecutableFilesystem {
	return firefoxExecutableFilesystem{
		locateExecutable: func(name string) (string, error) {
			path, found := fixture.locations[name]
			if !found {
				return "", os.ErrNotExist
			}

			return path, nil
		},
		resolveSymlinks: func(path string) (string, error) {
			resolved, found := fixture.resolutions[path]
			if !found {
				return "", os.ErrNotExist
			}

			return resolved, nil
		},
		inspectPath: func(path string) (os.FileInfo, error) {
			information, found := fixture.paths[path]
			if !found {
				return nil, os.ErrNotExist
			}

			return information, nil
		},
		checkAccess: func(string, uint32) error {
			return fixture.accessError
		},
	}
}

func acceptTestFirefoxExecutable(path string, required bool) (string, error) {
	if path == "" && required {
		return "/usr/bin/firefox-esr", nil
	}

	return path, nil
}

func testFirefoxLaunch(path string) BrowserLaunch {
	return BrowserLaunch{ExecPath: path, executableResolver: acceptTestFirefoxExecutable}
}

func TestResolveTrustedFirefoxExecutableAcceptsDistroSymlinkAndDiscovery(t *testing.T) {
	fixture := newFirefoxExecutableFixture()
	for _, configuredPath := range []string{"/usr/bin/firefox-esr", ""} {
		path, err := resolveTrustedFirefoxExecutable(
			configuredPath,
			true,
			fixture.filesystem(),
		)
		if err != nil || path != "/usr/bin/firefox-esr" {
			t.Fatalf("trusted Firefox path = %q, err = %v", path, err)
		}
	}
}

func TestResolveTrustedFirefoxExecutableDefersMissingDiscovery(t *testing.T) {
	fixture := newFirefoxExecutableFixture()
	fixture.locations = map[string]string{}
	path, err := resolveTrustedFirefoxExecutable("", false, fixture.filesystem())
	if err != nil || path != "" {
		t.Fatalf("optional Firefox path = %q, err = %v", path, err)
	}
	if _, err := resolveTrustedFirefoxExecutable("", true, fixture.filesystem()); err == nil ||
		!strings.Contains(err.Error(), "locate firefox") {
		t.Fatalf("required discovery error = %v", err)
	}
}

type firefoxExecutableRejectionCase struct {
	name   string
	path   string
	mutate func(*firefoxExecutableFixture)
	want   string
}

func firefoxPathRejectionCases() []firefoxExecutableRejectionCase {
	return []firefoxExecutableRejectionCase{
		{name: "launcher name", path: "/usr/bin/true", want: "launcher path"},
		{name: "noncanonical", path: " /usr/bin/firefox-esr ", want: "not canonical"},
		{
			name: "resolve",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				fixture.resolutions = map[string]string{}
			},
			want: "resolve firefox",
		},
		{
			name: "resolved relative",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				fixture.resolutions["/usr/bin/firefox-esr"] = "usr/lib/firefox-esr/firefox-esr"
			},
			want: "not absolute and clean",
		},
		{
			name: "missing original",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				delete(fixture.paths, "/usr/bin/firefox-esr")
			},
			want: "inspect firefox",
		},
		{
			name: "unknown ownership",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				information := fixture.paths["/usr/bin/firefox-esr"]
				information.sys = struct{}{}
				fixture.paths["/usr/bin/firefox-esr"] = information
			},
			want: "ownership",
		},
		{
			name: "untrusted link",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				information := fixture.paths["/usr/bin/firefox-esr"]
				information.uid = 1000
				fixture.paths["/usr/bin/firefox-esr"] = information
			},
			want: "not root-owned",
		},
	}
}

func firefoxPermissionRejectionCases() []firefoxExecutableRejectionCase {
	return []firefoxExecutableRejectionCase{
		{
			name: "writable parent",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				information := fixture.paths["/usr/bin"]
				information.mode = os.ModeDir | 0o775
				fixture.paths["/usr/bin"] = information
			},
			want: "group- or other-writable",
		},
		{
			name: "untrusted target",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				information := fixture.paths["/usr/lib/firefox-esr/firefox-esr"]
				information.uid = 1000
				fixture.paths["/usr/lib/firefox-esr/firefox-esr"] = information
			},
			want: "not root-owned",
		},
		{
			name: "writable target",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				information := fixture.paths["/usr/lib/firefox-esr/firefox-esr"]
				information.mode = 0o775
				fixture.paths["/usr/lib/firefox-esr/firefox-esr"] = information
			},
			want: "group- or other-writable",
		},
		{
			name: "directory target",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				information := fixture.paths["/usr/lib/firefox-esr/firefox-esr"]
				information.mode = os.ModeDir | 0o755
				fixture.paths["/usr/lib/firefox-esr/firefox-esr"] = information
			},
			want: "regular unprivileged executable",
		},
		{
			name: "nonexecutable target",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				information := fixture.paths["/usr/lib/firefox-esr/firefox-esr"]
				information.mode = 0o644
				fixture.paths["/usr/lib/firefox-esr/firefox-esr"] = information
			},
			want: "regular unprivileged executable",
		},
		{
			name: "setid target",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				information := fixture.paths["/usr/lib/firefox-esr/firefox-esr"]
				information.mode = os.ModeSetuid | 0o755
				fixture.paths["/usr/lib/firefox-esr/firefox-esr"] = information
			},
			want: "regular unprivileged executable",
		},
		{
			name: "inaccessible",
			path: "/usr/bin/firefox-esr",
			mutate: func(fixture *firefoxExecutableFixture) {
				fixture.accessError = syscall.EACCES
			},
			want: "not executable by this crawler",
		},
	}
}

func TestResolveTrustedFirefoxExecutableRejectsUntrustedPaths(t *testing.T) {
	tests := append(firefoxPathRejectionCases(), firefoxPermissionRejectionCases()...)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newFirefoxExecutableFixture()
			if test.mutate != nil {
				test.mutate(fixture)
			}
			_, err := resolveTrustedFirefoxExecutable(test.path, true, fixture.filesystem())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestBrowserLaunchRechecksExecutableResolution(t *testing.T) {
	calls := 0
	launch := BrowserLaunch{
		ExecPath: "/usr/bin/firefox-esr",
		executableResolver: func(path string, required bool) (string, error) {
			calls++
			if path != "/usr/bin/firefox-esr" || !required {
				t.Fatalf("resolve executable = %q/%v", path, required)
			}

			return path, nil
		},
	}
	path, err := firefoxBinary(launch)
	if err != nil || path != launch.ExecPath || calls != 1 {
		t.Fatalf("Firefox recheck = %q/%d, err = %v", path, calls, err)
	}
}

func TestOperatingSystemFirefoxExecutableFilesystemIsComplete(t *testing.T) {
	filesystem := operatingSystemFirefoxExecutableFilesystem()
	if filesystem.locateExecutable == nil || filesystem.resolveSymlinks == nil ||
		filesystem.inspectPath == nil || filesystem.checkAccess == nil {
		t.Fatal("operating-system Firefox executable filesystem is incomplete")
	}
	if _, err := filesystem.inspectPath(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("operating-system inspection accepted a missing path")
	}
}

func TestBrowserExecutableResolverErrorIsPreserved(t *testing.T) {
	sentinel := errors.New("trust rejected")
	launch := BrowserLaunch{executableResolver: func(string, bool) (string, error) {
		return "", sentinel
	}}
	if _, err := firefoxBinary(launch); !errors.Is(err, sentinel) {
		t.Fatalf("Firefox resolver error = %v", err)
	}
}
