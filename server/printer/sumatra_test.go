package printer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An explicitly configured absolute path wins, and comes back absolute — the whole
// point, since a relative arg 0 would send exec.Command through LookPath.
func TestFindSumatraConfiguredAbsolute(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, sumatraExe)
	if err := os.WriteFile(want, []byte("stub"), 0755); err != nil {
		t.Fatal(err)
	}
	got, tried := findSumatra(want, "")
	if got != want {
		t.Errorf("got %q, want %q (tried %v)", got, want, tried)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("resolved path must be absolute, got %q", got)
	}
}

// A configured path that does not exist falls through to the rest of the chain
// rather than being handed to exec as-is.
func TestFindSumatraConfiguredMissingFallsThrough(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope", sumatraExe)
	got, tried := findSumatra(missing, "")
	if got == missing {
		t.Errorf("a nonexistent configured path should not be returned: %q", got)
	}
	if len(tried) < 2 {
		t.Errorf("expected the search to continue past the configured path, tried=%v", tried)
	}
}

// The working directory is still searched (v1's behavior), and still yields an
// absolute path.
func TestFindSumatraWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	want := filepath.Join(dir, sumatraExe)
	if err := os.WriteFile(want, []byte("stub"), 0755); err != nil {
		t.Fatal(err)
	}
	got, tried := findSumatra("", "")
	abs, _ := filepath.EvalSymlinks(got)
	wantAbs, _ := filepath.EvalSymlinks(want)
	if abs != wantAbs {
		t.Errorf("got %q, want %q (tried %v)", got, want, tried)
	}
}

// Nothing found anywhere: the error has to name where we looked, because the fix
// for an operator is "put the file in one of these".
func TestSumatraNotFoundNamesLocations(t *testing.T) {
	err := sumatraNotFound([]string{"/a/SumatraPDF.exe", "%PATH%"})
	if !strings.Contains(err.Error(), "/a/SumatraPDF.exe") || !strings.Contains(err.Error(), "%PATH%") {
		t.Errorf("error should list every location tried, got: %v", err)
	}
}
