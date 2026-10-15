package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// runRecordingsComplete runs scripts/recordings-complete.sh over a directory
// of phrasings written for the test, and reports its output and exit code.
func runRecordingsComplete(t *testing.T, files map[string]string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("sh", filepath.Join(repoRootFromCaller(t), "scripts", "recordings-complete.sh"), dir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return string(out), 0
	}
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("running the script: %v\n%s", err, out)
	}
	return string(out), exitErr.ExitCode()
}

const recordedCase = `{
  "id": "a",
  "provider": "anthropic",
  "model": "m",
  "task": "classify",
  "message": "recordame comprar café mañana a las 10",
  "response": "{\"type\":\"task\"}",
  "expect": {}
}
`

func TestRecordingsCompleteFailsOnPendingCase(t *testing.T) {
	out, code := runRecordingsComplete(t, map[string]string{
		"a.json": recordedCase,
		"b.json": "{\n  \"id\": \"b\",\n  \"task\": \"classify\",\n  \"message\": \"hola\",\n  \"record\": true,\n  \"expect\": {}\n}\n",
	})
	if code != 1 || !strings.Contains(out, "b.json") || strings.Contains(out, "a.json") {
		t.Fatalf("exit %d, output %q: want exit 1 naming only the awaiting b.json", code, out)
	}
}

func TestRecordingsCompletePassesARecordedSet(t *testing.T) {
	if out, code := runRecordingsComplete(t, map[string]string{"a.json": recordedCase, "notes.md": "record: true"}); code != 0 {
		t.Fatalf("exit %d, output %q: want a recorded set (and a non-json file) to pass", code, out)
	}
	// "record": false is not awaiting anything.
	if out, code := runRecordingsComplete(t, map[string]string{"a.json": strings.Replace(recordedCase, `"expect"`, `"record": false, "expect"`, 1)}); code != 0 {
		t.Fatalf("exit %d, output %q: want record false to pass", code, out)
	}
}

func TestRecordingsCompleteRejectsVaultLikeContent(t *testing.T) {
	for name, leak := range map[string]string{
		"a unix home path":    "/home/pablo/vault/notes.md",
		"a macOS home path":   "/Users/pablo/Documents",
		"a windows path":      `C:\\Users\\pablo\\vault`,
		"a ULID":              "01JC3Z8Q4RK7M2N5P6V9X0ABCD",
		"an e-mail address":   "escribile a pablo@example.com",
		"the vault directory": "see .nooma/nooma.db",
	} {
		t.Run(name, func(t *testing.T) {
			body := strings.Replace(recordedCase, "recordame comprar café mañana a las 10", leak, 1)
			out, code := runRecordingsComplete(t, map[string]string{"leaky.json": body})
			if code != 1 || !strings.Contains(out, "leaky.json") {
				t.Fatalf("exit %d, output %q: want exit 1 naming leaky.json", code, out)
			}
		})
	}
	// Words that only look like paths or ids are fine.
	body := strings.Replace(recordedCase, "recordame comprar café mañana a las 10", "mandá y/o llamá a Ana, ya no/sí (1/2) HOLA", 1)
	if out, code := runRecordingsComplete(t, map[string]string{"fine.json": body}); code != 0 {
		t.Fatalf("exit %d, output %q: want ordinary text to pass", code, out)
	}
}

// TestRecordingsCompleteScript pins where the gate runs and why it is not in
// check-all (design D1c): the step is in ci.yml's required lint job, doc 06 §6
// says why check-all leaves it out, and CLAUDE.md states the exception.
func TestRecordingsCompleteScript(t *testing.T) {
	root := repoRootFromCaller(t)
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}

	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal([]byte(read(".github/workflows/ci.yml")), &wf); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, step := range wf.Jobs["lint"].Steps {
		found = found || strings.Contains(step.Run, "scripts/recordings-complete.sh")
	}
	if !found {
		t.Error("ci.yml's lint job (a required check) has no step running scripts/recordings-complete.sh")
	}

	if strings.Contains(read("Makefile"), "recordings-complete") {
		t.Error("check-all runs recordings-complete.sh: it must pass while phrasings still await their recording")
	}
	for _, doc := range []string{"docs/06-harness.md", "CLAUDE.md"} {
		if !strings.Contains(read(doc), "recordings-complete.sh") {
			t.Errorf("%s does not state the check-all exception for recordings-complete.sh", doc)
		}
	}
}

// A set that is not there is not a recorded set: a renamed directory must not
// turn the gate into a pass that checks nothing.
func TestRecordingsCompleteFailsOnAMissingOrEmptySet(t *testing.T) {
	cmd := exec.Command("sh", filepath.Join(repoRootFromCaller(t), "scripts", "recordings-complete.sh"), filepath.Join(t.TempDir(), "renamed"))
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "FAIL") {
		t.Fatalf("a missing directory = %v, output %q: want exit 1 and FAIL", err, out)
	}
	if out, code := runRecordingsComplete(t, map[string]string{"notes.md": "no phrasings here"}); code != 1 || !strings.Contains(out, "no phrasings") {
		t.Fatalf("an empty set: exit %d, output %q: want exit 1 saying there are no phrasings", code, out)
	}
}
