package printer

// Windows printing goes through SumatraPDF.exe, an external helper that cannot be
// embedded the way the fonts and logo are. Finding it is the whole problem.
//
// v1 worked by accident: it called filepath.Abs("SumatraPDF.exe"), which resolved
// against the working directory (where an operator had hand-placed a copy) and,
// because the result contained a separator, made exec.Command skip exec.LookPath
// entirely. v2 passed the bare name instead, so LookPath ran — and since Go 1.19 a
// LookPath hit in the current directory is REFUSED (exec.ErrDot), leaving only
// %PATH%. v2 had also removed every other reason to run from that folder.
//
// So: search a few sensible places, and always hand exec.Command an absolute path.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const sumatraExe = "SumatraPDF.exe"

// findSumatra resolves SumatraPDF.exe to an absolute path. It returns the places
// it looked so a failure can say exactly where to put the file.
func findSumatra(configured string, appDir string) (string, []string) {
	var tried []string
	try := func(p string) string {
		if p == "" {
			return ""
		}
		tried = append(tried, p)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			abs, err := filepath.Abs(p)
			if err != nil {
				return p
			}
			return abs
		}
		return ""
	}

	// 1. Configured explicitly. A bare name resolves under the app directory;
	//    an absolute path is honored as given.
	if configured != "" {
		if !filepath.IsAbs(configured) && appDir != "" {
			configured = filepath.Join(appDir, configured)
		}
		if hit := try(configured); hit != "" {
			return hit, tried
		}
	}
	// 2. Beside the running binary — where build.sh drops it.
	if exe, err := os.Executable(); err == nil {
		if hit := try(filepath.Join(filepath.Dir(exe), sumatraExe)); hit != "" {
			return hit, tried
		}
	}
	// 3. The app directory.
	if appDir != "" {
		if hit := try(filepath.Join(appDir, sumatraExe)); hit != "" {
			return hit, tried
		}
	}
	// 4. The working directory.
	if cwd, err := filepath.Abs(sumatraExe); err == nil {
		if hit := try(cwd); hit != "" {
			return hit, tried
		}
	}
	// 5. A real PATH install.
	tried = append(tried, "%PATH%")
	if hit, err := exec.LookPath(sumatraExe); err == nil {
		if abs, aerr := filepath.Abs(hit); aerr == nil {
			return abs, tried
		}
		return hit, tried
	}
	return "", tried
}

// sumatraNotFound builds an error naming every location searched.
func sumatraNotFound(tried []string) error {
	return fmt.Errorf("%s not found — looked in: %s", sumatraExe, strings.Join(tried, ", "))
}
