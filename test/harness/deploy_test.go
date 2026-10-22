package harness

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// No Docker runs here (non-negotiable 5 keeps tests off the network, and CI
// has no daemon to build with), so deploy/ is checked as data: compose.yml is
// parsed as YAML and the Dockerfile read as text, and each test pins one of
// the decisions docs/08-deploy.md states. These prove the files say what the
// doc says; they do not prove Docker accepts them.

type composeFile struct {
	Name     string                    `yaml:"name"`
	Services map[string]composeService `yaml:"services"`
	Networks map[string]struct {
		External bool `yaml:"external"`
	} `yaml:"networks"`
}

type composeService struct {
	Build struct {
		Context    string `yaml:"context"`
		Dockerfile string `yaml:"dockerfile"`
	} `yaml:"build"`
	Image       string            `yaml:"image"`
	Restart     string            `yaml:"restart"`
	EnvFile     string            `yaml:"env_file"`
	Environment map[string]string `yaml:"environment"`
	Ports       []string          `yaml:"ports"`
	Volumes     []string          `yaml:"volumes"`
	Networks    []string          `yaml:"networks"`
}

func readCompose(t *testing.T) composeFile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), "deploy", "docker", "compose.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var c composeFile
	if err := yaml.UnmarshalWithOptions(b, &c, yaml.Strict()); err != nil {
		t.Fatalf("compose.yml does not parse into the shape this test knows: %v", err)
	}
	if len(c.Services) == 0 {
		t.Fatal("compose.yml declares no services")
	}
	return c
}

// TestDeploy_ComposeNeverExposesNooma: every published port is bound to the
// host's loopback, nothing asks nginx-proxy to publish it, and it joins no
// shared or external network.
func TestDeploy_ComposeNeverExposesNooma(t *testing.T) {
	c := readCompose(t)
	for name, n := range c.Networks {
		if name == "proxy" || n.External {
			t.Errorf("network %q: Nooma must have its own network, never the proxy's or an external one", name)
		}
	}
	for name, s := range c.Services {
		if len(s.Ports) == 0 {
			t.Errorf("service %s publishes no port; the UI is reached through 127.0.0.1", name)
		}
		for _, p := range s.Ports {
			if !strings.HasPrefix(p, "127.0.0.1:") {
				t.Errorf("service %s publishes %q; only 127.0.0.1:<port>:<port> is allowed", name, p)
			}
		}
		for k := range s.Environment {
			if strings.HasPrefix(k, "VIRTUAL_") || strings.HasPrefix(k, "LETSENCRYPT_") {
				t.Errorf("service %s sets %s, which asks nginx-proxy to publish it", name, k)
			}
		}
		for _, n := range s.Networks {
			if _, own := c.Networks[n]; !own || n == "proxy" {
				t.Errorf("service %s joins network %q, which is not this project's own", name, n)
			}
		}
	}
}

// TestDeploy_ComposeRunsTheServiceTheBackupScriptExpects: the service, its
// mounts, its secrets file and its build are the ones docs/08 and
// nooma-backup.sh rely on.
func TestDeploy_ComposeRunsTheServiceTheBackupScriptExpects(t *testing.T) {
	c := readCompose(t)
	s, ok := c.Services["nooma"]
	if !ok {
		t.Fatal(`compose.yml has no "nooma" service, which nooma-backup.sh execs into`)
	}
	if s.Restart != "unless-stopped" {
		t.Errorf("restart = %q, want unless-stopped", s.Restart)
	}
	if s.EnvFile != ".env" {
		t.Errorf("env_file = %q, want .env (deploy/docker/.env, gitignored)", s.EnvFile)
	}
	if s.Environment["TZ"] == "" {
		t.Error("TZ is not set; the binary's clock reads the process zone")
	}
	if s.Build.Context != "../.." || s.Build.Dockerfile != "deploy/docker/Dockerfile" {
		t.Errorf("build = %+v, want the repository root and deploy/docker/Dockerfile", s.Build)
	}
	for _, want := range []string{"/srv/nooma/data:/data", "/srv/nooma/backups:/backups"} {
		if !contains(s.Volumes, want) {
			t.Errorf("volumes %v lack %s", s.Volumes, want)
		}
	}

	script := readRepoFile(t, "deploy/backup/nooma-backup.sh")
	if !strings.Contains(script, "exec -T nooma nooma backup /data/vault /backups") {
		t.Error("nooma-backup.sh does not back up /data/vault into /backups in the nooma service")
	}
	if !strings.Contains(readRepoFile(t, "deploy/docker/Dockerfile"), "ENV NOOMA_VAULT=/data/vault") {
		t.Error("the image does not point NOOMA_VAULT at /data/vault, the vault the script backs up")
	}
}

// TestDeploy_TheSecretsFileIsIgnoredAndItsExampleIsNot: a git pull can never
// conflict with the droplet's secrets, and the example ships.
func TestDeploy_TheSecretsFileIsIgnoredAndItsExampleIsNot(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := repoRootFromCaller(t)
	ignored := func(path string) bool {
		cmd := exec.Command("git", "check-ignore", "-q", "--no-index", path)
		cmd.Dir = root
		return cmd.Run() == nil
	}
	if !ignored("deploy/docker/.env") {
		t.Error("deploy/docker/.env is not gitignored")
	}
	if ignored("deploy/docker/.env.example") {
		t.Error("deploy/docker/.env.example is gitignored, so it would never ship")
	}
	if !strings.Contains(readRepoFile(t, ".dockerignore"), "deploy/docker/.env") {
		t.Error(".dockerignore does not keep deploy/docker/.env out of the build context")
	}
}

// TestDeploy_DockerfileBuildsAStaticBinaryThatRunsAsNonRoot pins the image's
// shape: a CGO-free, trimmed build of the one command, run as a non-root user
// with no shell.
func TestDeploy_DockerfileBuildsAStaticBinaryThatRunsAsNonRoot(t *testing.T) {
	df := readRepoFile(t, "deploy/docker/Dockerfile")
	for _, want := range []string{"CGO_ENABLED=0", "-trimpath", "./cmd/nooma", "-tags timetzdata",
		"FROM gcr.io/distroless/static", "USER 65532:65532", `ENTRYPOINT ["nooma"]`} {
		if !strings.Contains(df, want) {
			t.Errorf("Dockerfile lacks %q", want)
		}
	}
	if strings.Contains(df, "USER root") {
		t.Error("Dockerfile switches to root")
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// runBackupScript runs nooma-backup.sh with stub docker and rclone commands
// that record their arguments and exit with the code named for them in
// fail ("docker", "rclone copy", "rclone delete"). It returns the script's
// output, its exit code and the recorded calls.
func runBackupScript(t *testing.T, remote string, fail string) (output string, code int, calls []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the backup script runs on a Linux host")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not on PATH")
	}

	bin := t.TempDir()
	callLog := filepath.Join(t.TempDir(), "calls")
	stub := `#!/bin/sh
echo "$(basename "$0") $*" >> "` + callLog + `"
case "$(basename "$0") $1" in "` + fail + `"*) exit 7 ;; esac
exit 0
`
	for _, name := range []string{"docker", "rclone"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command(bash, filepath.Join(repoRootFromCaller(t), "deploy", "backup", "nooma-backup.sh"))
	cmd.Env = []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "NOOMA_RCLONE_REMOTE=" + remote,
		"NOOMA_COMPOSE_FILE=/c/compose.yml", "NOOMA_BACKUP_DIR=/b"}
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running the script: %v", err)
	}
	if b, err := os.ReadFile(callLog); err == nil {
		calls = strings.Split(strings.TrimSpace(string(b)), "\n")
	}
	return string(out), code, calls
}

// TestBackupScript_SnapshotsThenUploadsThenPrunesTheRemote: the success path,
// in order, with the remote taken from the variable.
func TestBackupScript_SnapshotsThenUploadsThenPrunesTheRemote(t *testing.T) {
	out, code, calls := runBackupScript(t, "crypt:", "none")
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	want := []string{
		"docker compose -f /c/compose.yml exec -T nooma nooma backup /data/vault /backups --keep 14",
		"rclone copy /b crypt: --include nooma-*.db --include nooma-*.yml",
		"rclone delete crypt: --min-age 30d --include nooma-*.db --include nooma-*.yml",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls =\n%s\nwant\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(out, "nooma-backup: ok") {
		t.Errorf("output %q lacks the ok log line", out)
	}
}

// TestBackupScript_AnyFailureStopsAndExitsNonZero: a failed step stops the
// run, logs FAILED and exits non-zero; nothing after it runs. A missing
// remote fails before anything runs.
func TestBackupScript_AnyFailureStopsAndExitsNonZero(t *testing.T) {
	cases := []struct {
		name, remote, fail string
		wantCalls          int
	}{
		{"no remote", "", "none", 0},
		{"the snapshot fails", "crypt:", "docker", 1},
		{"the upload fails", "crypt:", "rclone copy", 2},
		{"the remote prune fails", "crypt:", "rclone delete", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code, calls := runBackupScript(t, tc.remote, tc.fail)
			if code == 0 {
				t.Fatalf("exit 0, want non-zero\n%s", out)
			}
			if len(calls) != tc.wantCalls {
				t.Errorf("%d calls %v, want %d", len(calls), calls, tc.wantCalls)
			}
			if !strings.Contains(out, "FAILED") || strings.Contains(out, "nooma-backup: ok") {
				t.Errorf("output %q: want a FAILED line and no ok line", out)
			}
		})
	}
}
