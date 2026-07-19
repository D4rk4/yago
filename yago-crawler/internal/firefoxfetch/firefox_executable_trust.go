package firefoxfetch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const firefoxExecuteAccess = 1

type firefoxExecutableFilesystem struct {
	locateExecutable func(string) (string, error)
	resolveSymlinks  func(string) (string, error)
	inspectPath      func(string) (os.FileInfo, error)
	checkAccess      func(string, uint32) error
}

func operatingSystemFirefoxExecutableFilesystem() firefoxExecutableFilesystem {
	return firefoxExecutableFilesystem{
		locateExecutable: exec.LookPath,
		resolveSymlinks:  filepath.EvalSymlinks,
		inspectPath:      os.Lstat,
		checkAccess:      syscall.Access,
	}
}

func resolveTrustedFirefoxExecutable(
	configuredPath string,
	required bool,
	filesystem firefoxExecutableFilesystem,
) (string, error) {
	candidate := configuredPath
	if candidate == "" {
		candidate = locateFirefoxExecutable(filesystem)
		if candidate == "" {
			if required {
				return "", fmt.Errorf("locate firefox: none of %v found on PATH", firefoxBinaries)
			}

			return "", nil
		}
	}
	canonicalPath, err := yagocrawlcontract.ParseCrawlerBrowserPath(candidate)
	if err != nil {
		return "", fmt.Errorf("validate firefox executable path %q: %w", candidate, err)
	}
	if canonicalPath != candidate {
		return "", fmt.Errorf("firefox executable path %q is not canonical", candidate)
	}
	resolvedPath, err := filesystem.resolveSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve firefox executable path %q: %w", candidate, err)
	}
	if err := inspectFirefoxExecutableChain(filesystem, candidate, false); err != nil {
		return "", err
	}
	if err := inspectFirefoxExecutableChain(filesystem, resolvedPath, true); err != nil {
		return "", err
	}
	if err := filesystem.checkAccess(candidate, firefoxExecuteAccess); err != nil {
		return "", fmt.Errorf(
			"firefox executable path %q is not executable by this crawler: %w",
			candidate,
			err,
		)
	}

	return candidate, nil
}

func locateFirefoxExecutable(filesystem firefoxExecutableFilesystem) string {
	for _, name := range firefoxBinaries {
		path, err := filesystem.locateExecutable(name)
		if err == nil {
			return path
		}
	}

	return ""
}

func inspectFirefoxExecutableChain(
	filesystem firefoxExecutableFilesystem,
	path string,
	requireExecutable bool,
) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("firefox executable path %q is not absolute and clean", path)
	}
	currentPath := path
	finalPath := true
	for {
		information, err := filesystem.inspectPath(currentPath)
		if err != nil {
			return fmt.Errorf("inspect firefox executable path %q: %w", currentPath, err)
		}
		if err := validateFirefoxExecutableInformation(
			currentPath,
			information,
			finalPath && requireExecutable,
		); err != nil {
			return err
		}
		if currentPath == string(filepath.Separator) {
			return nil
		}
		currentPath = filepath.Dir(currentPath)
		finalPath = false
	}
}

func validateFirefoxExecutableInformation(
	path string,
	information os.FileInfo,
	requireExecutable bool,
) error {
	ownership, ok := information.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("inspect firefox executable ownership for %q", path)
	}
	if ownership.Uid != 0 {
		return fmt.Errorf("firefox executable path %q is not root-owned", path)
	}
	mode := information.Mode()
	if mode&os.ModeSymlink == 0 && mode.Perm()&0o022 != 0 {
		return fmt.Errorf("firefox executable path %q is group- or other-writable", path)
	}
	if requireExecutable && (!mode.IsRegular() || mode.Perm()&0o111 == 0 ||
		mode&(os.ModeSetuid|os.ModeSetgid) != 0) {
		return fmt.Errorf(
			"firefox executable path %q is not a regular unprivileged executable",
			path,
		)
	}

	return nil
}
