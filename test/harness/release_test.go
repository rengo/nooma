package harness

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
)

// The release pipeline (ADR-0060) runs only on a tag push, so CI never
// exercises it on a pull request. These tests read .goreleaser.yaml and
// release.yml as data and pin the decisions ADR-0060 states, the same way
// deploy_test.go reads compose.yml: they prove the files say what the ADR
// says, not that goreleaser or GitHub accepts them.

type goreleaserFile struct {
	Builds []struct {
		ID      string   `yaml:"id"`
		Main    string   `yaml:"main"`
		Env     []string `yaml:"env"`
		Flags   []string `yaml:"flags"`
		Tags    []string `yaml:"tags"`
		Ldflags []string `yaml:"ldflags"`
		Goos    []string `yaml:"goos"`
		Goarch  []string `yaml:"goarch"`
		Goarm   []string `yaml:"goarm"`
		Ignore  []struct {
			Goos   string `yaml:"goos"`
			Goarch string `yaml:"goarch"`
		} `yaml:"ignore"`
	} `yaml:"builds"`
	Archives []struct {
		Formats         []string `yaml:"formats"`
		Files           []string `yaml:"files"`
		FormatOverrides []struct {
			Goos    string   `yaml:"goos"`
			Formats []string `yaml:"formats"`
		} `yaml:"format_overrides"`
	} `yaml:"archives"`
	Checksum struct {
		NameTemplate string `yaml:"name_template"`
	} `yaml:"checksum"`
	Signs []struct {
		Cmd       string   `yaml:"cmd"`
		Artifacts string   `yaml:"artifacts"`
		Args      []string `yaml:"args"`
	} `yaml:"signs"`
	Dockers []struct {
		Dockerfile string   `yaml:"dockerfile"`
		Images     []string `yaml:"images"`
		Tags       []string `yaml:"tags"`
		Platforms  []string `yaml:"platforms"`
	} `yaml:"dockers_v2"`
}

func readGoreleaser(t *testing.T) goreleaserFile {
	t.Helper()
	var g goreleaserFile
	if err := yaml.Unmarshal([]byte(readRepoFile(t, ".goreleaser.yaml")), &g); err != nil {
		t.Fatalf(".goreleaser.yaml does not parse: %v", err)
	}
	if len(g.Builds) != 1 {
		t.Fatalf(".goreleaser.yaml declares %d builds, want exactly one", len(g.Builds))
	}
	return g
}

// goreleaserTargets is the build's goos x goarch product minus its ignores.
func goreleaserTargets(g goreleaserFile) []string {
	b := g.Builds[0]
	var out []string
	for _, os := range b.Goos {
		for _, arch := range b.Goarch {
			ignored := false
			for _, ig := range b.Ignore {
				ignored = ignored || (ig.Goos == os && ig.Goarch == arch)
			}
			if !ignored {
				out = append(out, os+"/"+arch)
			}
		}
	}
	slices.Sort(out)
	return out
}

// makefileCrossTargets reads the Makefile's CROSS_TARGETS list.
func makefileCrossTargets(t *testing.T) []string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^CROSS_TARGETS := (.+)$`).FindStringSubmatch(readRepoFile(t, "Makefile"))
	if m == nil {
		t.Fatal("the Makefile has no CROSS_TARGETS line")
	}
	out := strings.Fields(m[1])
	slices.Sort(out)
	return out
}

// adr0013Targets reads the matrix from the first code block of ADR-0013's
// Decision section.
func adr0013Targets(t *testing.T) []string {
	t.Helper()
	adr := readRepoFile(t, "docs/adr/0013-cross-compile-targets.md")
	_, decision, ok := strings.Cut(adr, "## Decision")
	if !ok {
		t.Fatal("ADR-0013 has no Decision section")
	}
	parts := strings.SplitN(decision, "```", 3)
	if len(parts) < 3 {
		t.Fatal("ADR-0013's Decision section has no code block")
	}
	out := strings.Fields(parts[1])
	slices.Sort(out)
	return out
}

// TestRelease_TargetsAreADR0013sMatrix: goreleaser publishes exactly the
// seven targets ADR-0013 names and `make cross-compile` builds.
func TestRelease_TargetsAreADR0013sMatrix(t *testing.T) {
	adr := adr0013Targets(t)
	if len(adr) != 7 {
		t.Fatalf("ADR-0013's matrix reads as %v, want seven targets", adr)
	}
	if got := makefileCrossTargets(t); !slices.Equal(got, adr) {
		t.Errorf("Makefile CROSS_TARGETS = %v, want ADR-0013's %v", got, adr)
	}
	if got := goreleaserTargets(readGoreleaser(t)); !slices.Equal(got, adr) {
		t.Errorf(".goreleaser.yaml targets = %v, want ADR-0013's %v", got, adr)
	}
}

// TestRelease_BuildIsStaticAndStampedLikeTheMakefile: CGO off, trimmed, the
// zone database embedded, and the version stamped through the same
// -X main.version= the Makefile uses, from the tag git describe would print.
func TestRelease_BuildIsStaticAndStampedLikeTheMakefile(t *testing.T) {
	b := readGoreleaser(t).Builds[0]
	if b.Main != "./cmd/nooma" {
		t.Errorf("main = %q, want ./cmd/nooma", b.Main)
	}
	if !slices.Contains(b.Env, "CGO_ENABLED=0") {
		t.Errorf("env %v lacks CGO_ENABLED=0", b.Env)
	}
	if !slices.Contains(b.Flags, "-trimpath") {
		t.Errorf("flags %v lack -trimpath", b.Flags)
	}
	if !slices.Equal(b.Tags, []string{"timetzdata"}) {
		t.Errorf("tags = %v, want [timetzdata]", b.Tags)
	}
	// linux/arm as the Makefile cross-compiles it: GOARM=7 is Go's default
	// off an ARM host, and the image's platform is linux/arm/v7.
	if !slices.Equal(b.Goarm, []string{"7"}) {
		t.Errorf("goarm = %v, want [7]", b.Goarm)
	}

	m := regexp.MustCompile(`(?m)^LDFLAGS := (.+)$`).FindStringSubmatch(readRepoFile(t, "Makefile"))
	if m == nil || m[1] != "-X main.version=$(VERSION)" {
		t.Fatalf("the Makefile's LDFLAGS changed (%q); this test pins goreleaser to it", m)
	}
	ld := strings.Join(b.Ldflags, " ")
	if !strings.Contains(ld, "-X main.version={{ .Tag }}") {
		t.Errorf("ldflags %q do not stamp main.version with the tag ({{ .Tag }}, v-prefixed like git describe)", ld)
	}
}

// TestRelease_ArchivesChecksumsAndSignature: tar.gz, zip on Windows, each with
// the license and README; one checksums.txt, signed keyless by cosign.
func TestRelease_ArchivesChecksumsAndSignature(t *testing.T) {
	g := readGoreleaser(t)
	if len(g.Archives) != 1 {
		t.Fatalf("%d archives, want one", len(g.Archives))
	}
	a := g.Archives[0]
	if !slices.Equal(a.Formats, []string{"tar.gz"}) {
		t.Errorf("formats = %v, want [tar.gz]", a.Formats)
	}
	if len(a.FormatOverrides) != 1 || a.FormatOverrides[0].Goos != "windows" ||
		!slices.Equal(a.FormatOverrides[0].Formats, []string{"zip"}) {
		t.Errorf("format_overrides = %+v, want zip on windows only", a.FormatOverrides)
	}
	for _, f := range []string{"LICENSE", "README.md"} {
		if !slices.Contains(a.Files, f) {
			t.Errorf("archive files %v lack %s", a.Files, f)
		}
	}
	if g.Checksum.NameTemplate != "checksums.txt" {
		t.Errorf("checksum name = %q, want checksums.txt", g.Checksum.NameTemplate)
	}
	if len(g.Signs) != 1 || g.Signs[0].Cmd != "cosign" || g.Signs[0].Artifacts != "checksum" {
		t.Fatalf("signs = %+v, want one cosign signature of the checksum", g.Signs)
	}
	args := strings.Join(g.Signs[0].Args, " ")
	for _, want := range []string{"sign-blob", "--yes"} {
		if !strings.Contains(args, want) {
			t.Errorf("cosign args %q lack %s", args, want)
		}
	}
	if strings.Contains(args, "--key") {
		t.Errorf("cosign args %q name a key; the signature is keyless (GitHub OIDC)", args)
	}
}

// TestRelease_ImageIsMultiArchAndLatestSkipsPrereleases: one GHCR image for
// the three Linux targets, tagged with the version, and :latest only when the
// tag has no prerelease suffix.
func TestRelease_ImageIsMultiArchAndLatestSkipsPrereleases(t *testing.T) {
	g := readGoreleaser(t)
	if len(g.Dockers) != 1 {
		t.Fatalf("%d dockers_v2 entries, want one", len(g.Dockers))
	}
	d := g.Dockers[0]
	if d.Dockerfile != "deploy/docker/Dockerfile.release" {
		t.Errorf("dockerfile = %q, want deploy/docker/Dockerfile.release", d.Dockerfile)
	}
	if !slices.Equal(d.Images, []string{"ghcr.io/rengo/nooma"}) {
		t.Errorf("images = %v, want [ghcr.io/rengo/nooma]", d.Images)
	}
	want := []string{"linux/amd64", "linux/arm64", "linux/arm/v7"}
	if !slices.Equal(d.Platforms, want) {
		t.Errorf("platforms = %v, want %v", d.Platforms, want)
	}
	if !slices.Equal(d.Tags, []string{"{{ .Version }}", "{{ if not .Prerelease }}latest{{ end }}"}) {
		t.Errorf("tags = %v, want the version, and latest only off a prerelease", d.Tags)
	}
}

// TestRelease_ImageKeepsTheSourceImagesRuntimeContract: the release image
// copies a prebuilt binary instead of compiling, and its runtime stage is the
// development image's: same base, user, vault, port, entrypoint and command.
func TestRelease_ImageKeepsTheSourceImagesRuntimeContract(t *testing.T) {
	src := readRepoFile(t, "deploy/docker/Dockerfile")
	rel := readRepoFile(t, "deploy/docker/Dockerfile.release")
	if strings.Contains(rel, "go build") {
		t.Error("Dockerfile.release compiles; it must copy goreleaser's binary")
	}
	if !strings.Contains(rel, "COPY $TARGETPLATFORM/nooma /usr/local/bin/nooma") {
		t.Error("Dockerfile.release does not copy the platform's prebuilt binary to /usr/local/bin/nooma")
	}
	runtime := func(df string) []string {
		i := strings.LastIndex(df, "\nFROM ")
		var out []string
		for _, line := range strings.Split(df[i+1:], "\n") {
			for _, kw := range []string{"FROM ", "ENV ", "USER ", "EXPOSE ", "ENTRYPOINT ", "CMD "} {
				if strings.HasPrefix(line, kw) {
					out = append(out, line)
				}
			}
		}
		return out
	}
	if got, want := runtime(rel), runtime(src); !slices.Equal(got, want) || len(want) < 6 {
		t.Errorf("runtime stage\n%v\nwant the source image's\n%v", got, want)
	}
	for _, dir := range []string{"/data", "/backups"} {
		if !regexp.MustCompile(`COPY --from=\S+ --chown=65532:65532 \S+ ` + dir + `\n`).MatchString(rel) {
			t.Errorf("Dockerfile.release does not create %s owned by 65532", dir)
		}
	}
}

// TestRelease_WorkflowRunsOnVersionTagsOnlyWithMinimalPermissions: a pull
// request or a merge to main never publishes, and the token can write
// releases, packages and an OIDC identity, nothing else.
func TestRelease_WorkflowRunsOnVersionTagsOnlyWithMinimalPermissions(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRootFromCaller(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w map[string]any
	if err := yaml.Unmarshal(b, &w); err != nil {
		t.Fatalf("release.yml does not parse: %v", err)
	}
	on, ok := w["on"].(map[string]any)
	if !ok {
		t.Fatalf("release.yml's on = %#v, want a mapping", w["on"])
	}
	if len(on) != 1 || on["push"] == nil {
		t.Errorf("release.yml triggers on %v, want push only", keys(on))
	}
	push, _ := on["push"].(map[string]any)
	if len(push) != 1 {
		t.Errorf("release.yml's push filters = %v, want tags only (no branches)", keys(push))
	}
	if tags, _ := push["tags"].([]any); len(tags) != 1 || tags[0] != "v*" {
		t.Errorf("release.yml's push tags = %v, want [v*]", push["tags"])
	}

	want := map[string]any{"contents": "write", "id-token": "write", "packages": "write"}
	perms, _ := w["permissions"].(map[string]any)
	if len(perms) != len(want) {
		t.Errorf("permissions = %v, want exactly %v", perms, want)
	}
	for k, v := range want {
		if perms[k] != v {
			t.Errorf("permissions[%s] = %v, want %v", k, perms[k], v)
		}
	}
	jobs, _ := w["jobs"].(map[string]any)
	for name, j := range jobs {
		if job, _ := j.(map[string]any); job["permissions"] != nil {
			t.Errorf("job %s sets its own permissions; the workflow's minimal set is the only one", name)
		}
	}
	if !strings.Contains(string(b), "goreleaser/goreleaser-action@") {
		t.Error("release.yml does not run goreleaser")
	}
}

// TestRelease_EverythingTheWorkflowRunsIsPinned: the workflow that can
// publish and sign as this repository runs no action by a movable tag, an
// exact goreleaser release, and base images by digest.
func TestRelease_EverythingTheWorkflowRunsIsPinned(t *testing.T) {
	wf := readRepoFile(t, ".github/workflows/release.yml")
	uses := regexp.MustCompile(`(?m)^\s*(?:- )?uses: (\S+)(.*)$`).FindAllStringSubmatch(wf, -1)
	if len(uses) == 0 {
		t.Fatal("release.yml uses no action")
	}
	pinned := regexp.MustCompile(`^[\w.-]+/[\w.-]+@[0-9a-f]{40}$`)
	for _, u := range uses {
		if !pinned.MatchString(u[1]) {
			t.Errorf("%s is not pinned to a full commit SHA", u[1])
		}
		if !regexp.MustCompile(`^\s+# v\d+\.\d+\.\d+$`).MatchString(u[2]) {
			t.Errorf("%s does not name its tag in a comment (# vX.Y.Z)", u[1])
		}
	}
	if !regexp.MustCompile(`(?m)^\s+version: "?v2\.\d+\.\d+"?$`).MatchString(wf) {
		t.Error("release.yml does not pin goreleaser to an exact v2.x.y release")
	}
	from := regexp.MustCompile(`(?m)^FROM (?:--platform=\S+ )?(\S+)`)
	for _, m := range from.FindAllStringSubmatch(readRepoFile(t, "deploy/docker/Dockerfile.release"), -1) {
		if !regexp.MustCompile(`@sha256:[0-9a-f]{64}$`).MatchString(m[1]) {
			t.Errorf("Dockerfile.release's base %s is not pinned by digest", m[1])
		}
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
