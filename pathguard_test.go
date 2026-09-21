package pathguard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// realTemp is a temporary directory in its resolved spelling (/var is a link on
// darwin), so expectations do not have to care.
func realTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p string) {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, at string) {
	t.Helper()
	if err := os.Symlink(target, at); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// caseInsensitive reports whether dir's filesystem folds case, APFS's default;
// the spelling tests mean nothing on a disk that does not.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	probe := filepath.Join(dir, "case-probe")
	mkdir(t, probe)
	defer func() { _ = os.Remove(probe) }()
	_, err := os.Stat(filepath.Join(dir, "CASE-PROBE"))
	return err == nil
}

func floorOf(t *testing.T, home string) []Place {
	t.Helper()
	f, err := Floor(home)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// The hole no comparison of end points closes: ~/.ssh is a real directory, but
// ~/.ssh/config links into a sync folder. A link planted as work/x →
// ~/.ssh/config resolves to the sync folder, which is neither ~/.ssh nor under
// it. Only the middle hop is, and it is a form.
func TestALinkedFileInsideARealFloorDirectoryIsRefused(t *testing.T) {
	home := realTemp(t)
	mkdir(t, filepath.Join(home, ".ssh"))
	synced := filepath.Join(realTemp(t), "sync", "config")
	write(t, synced)
	link(t, synced, filepath.Join(home, ".ssh", "config"))
	work := realTemp(t)
	planted := filepath.Join(work, "x")
	link(t, filepath.Join(home, ".ssh", "config"), planted)

	forms := Forms(planted)
	if !slices.Contains(forms, filepath.Join(home, ".ssh", "config")) || !slices.Contains(forms, synced) {
		t.Errorf("forms = %v, want the middle hop and the final path", forms)
	}
	if reason, _ := Check(floorOf(t, home), planted); reason != "sensitive_path" {
		t.Errorf("Check(planted link) reason = %q, want sensitive_path", reason)
	}
}

// ".." after a link climbs out of the link's target, not out of the link's
// name: p2 → a/b, so p2/../q is a/q. Cleaning first would give base/q.
func TestALinkIsFollowedBeforeAnythingClimbsOutOfIt(t *testing.T) {
	base := realTemp(t)
	mkdir(t, filepath.Join(base, "a", "b"))
	link(t, filepath.Join(base, "a", "b"), filepath.Join(base, "p2"))
	got := Forms(base + "/p2/../q")
	if want := filepath.Join(base, "a", "q"); !slices.Contains(got, want) {
		t.Errorf("forms = %v, want %s among them", got, want)
	}
}

func TestALinkThatNeverEndsIsRefused(t *testing.T) {
	d := realTemp(t)
	link(t, filepath.Join(d, "l2"), filepath.Join(d, "l1"))
	link(t, filepath.Join(d, "l1"), filepath.Join(d, "l2"))
	if _, ok := forms(filepath.Join(d, "l1", "x")); ok {
		t.Error("a link loop resolved")
	}
	if reason, _ := Check(nil, filepath.Join(d, "l1", "x")); reason != "unresolvable_path" {
		t.Errorf("reason = %q, want unresolvable_path", reason)
	}
}

// "/" and /private/var refuse a work directory that is them, not everything
// below them: every per-user temporary directory lies under /private/var.
func TestAnExactPlaceDoesNotRefuseWhatLiesBelowIt(t *testing.T) {
	places := []Place{{Path: "/", Exact: true, Why: "root"}, {Path: "/private/var", Exact: true, Why: "var"}}
	if _, why := Check(places, t.TempDir()); why != "" {
		t.Errorf("a temporary directory was refused: %s", why)
	}
	if _, why := Check(places, "/"); why != "root" {
		t.Errorf("/ itself: %q", why)
	}
}

func TestAPlaceRefusesWhatIsInsideItNotASiblingSharingItsPrefix(t *testing.T) {
	own := filepath.Join(realTemp(t), "data")
	mkdir(t, filepath.Join(own, "sub"))
	places := []Place{{Path: own, Why: "own"}}
	for _, p := range []string{own, filepath.Join(own, "sub"), filepath.Join(own, "not-yet")} {
		if _, why := Check(places, p); why != "own" {
			t.Errorf("Check(%q) = %q, want refused", p, why)
		}
	}
	sibling := own + "-evil"
	mkdir(t, sibling)
	if _, why := Check(places, sibling, filepath.Join(sibling, "c.pcap")); why != "" {
		t.Errorf("a sibling sharing the prefix was refused: %s", why)
	}
	if withinFold("/data-evil/c.pcap", "/data") || !withinFold("/data/c.pcap", "/data") || !withinFold("/data", "/data") {
		t.Error("withinFold admits siblings or refuses itself")
	}
}

// Identity catches every spelling of a place that exists. On APFS a case
// variant names the same directory; through a link, any path does.
func TestAPlaceIsFoundThroughAnySpellingOfIt(t *testing.T) {
	root := realTemp(t)
	own := filepath.Join(root, "Guarded")
	mkdir(t, filepath.Join(own, "inner"))
	places := []Place{{Path: own, Why: "own"}}
	elsewhere := filepath.Join(realTemp(t), "door")
	link(t, own, elsewhere)
	if _, why := Check(places, filepath.Join(elsewhere, "inner")); why != "own" {
		t.Errorf("through a link: %q", why)
	}
	if !caseInsensitive(t, root) {
		t.Skip("case-sensitive filesystem: another spelling is another directory")
	}
	for _, spelled := range []string{filepath.Join(root, "GUARDED", "inner"), filepath.Join(strings.ToUpper(root), "guarded")} {
		if _, why := Check(places, spelled); why != "own" {
			t.Errorf("Check(%q) = %q, want refused", spelled, why)
		}
	}
}

// A floor directory that is itself a link (~/.ssh into a sync folder on the
// machine that found it): refused named through the link, at its target, and
// through a link planted elsewhere that points into it.
func TestAFloorDirectoryThatIsALinkIsRefusedAtBothEnds(t *testing.T) {
	home := realTemp(t)
	real := filepath.Join(realTemp(t), "synced-ssh")
	key := filepath.Join(real, "id_rsa")
	write(t, key)
	link(t, real, filepath.Join(home, ".ssh"))
	planted := filepath.Join(realTemp(t), "innocent.pcap")
	link(t, key, planted)
	floor := floorOf(t, home)
	for _, p := range []string{filepath.Join(home, ".ssh", "id_rsa"), key, planted} {
		if reason, _ := Check(floor, p); reason != "sensitive_path" {
			t.Errorf("Check(%q) reason = %q, want sensitive_path", p, reason)
		}
	}
}

// The name comparison stays for a place that does not exist yet: a write that
// would create ~/.aws under another spelling is refused all the same.
func TestTheNameComparisonCoversAPlaceThatDoesNotExistYet(t *testing.T) {
	home := realTemp(t)
	if reason, _ := Check(floorOf(t, home), filepath.Join(home, ".AWS", "credentials")); reason != "sensitive_path" {
		t.Errorf("reason = %q, want sensitive_path", reason)
	}
}

// The home directory is refused as itself, found by identity under any
// spelling — /USERS/<name> passed the name comparison of the copies.
func TestTheHomeDirectoryIsRefusedAsItselfUnderAnySpelling(t *testing.T) {
	home := realTemp(t)
	floor := floorOf(t, home)
	if reason, _ := Check(floor, home); reason != "home_dir" {
		t.Errorf("home itself: %q", reason)
	}
	mkdir(t, filepath.Join(home, "project"))
	if _, why := Check(floor, filepath.Join(home, "project")); why != "" {
		t.Errorf("a directory inside home was refused: %s", why)
	}
	if caseInsensitive(t, home) {
		if reason, _ := Check(floor, strings.ToUpper(home)); reason != "home_dir" {
			t.Errorf("home in capitals: %q", reason)
		}
	}
}

func TestSystemTreesAreRefusedUnderAnySpelling(t *testing.T) {
	floor := floorOf(t, realTemp(t))
	if reason, _ := Check(floor, "/usr/local"); reason != "system_dir" {
		t.Errorf("/usr/local: %q", reason)
	}
	if _, err := os.Stat("/usr"); err == nil && caseInsensitive(t, os.TempDir()) {
		if reason, _ := Check(floor, "/USR/local"); reason != "system_dir" {
			t.Errorf("/USR/local: %q", reason)
		}
	}
}

func TestFloorWithoutAHomeIsAnError(t *testing.T) {
	for _, h := range []string{"", "  "} {
		if _, err := Floor(h); err != ErrNoHome {
			t.Errorf("Floor(%q) err = %v, want ErrNoHome", h, err)
		}
	}
	if _, err := Local(""); err != ErrNoHome {
		t.Errorf("Local(\"\") err = %v", err)
	}
}

type runtimeLists struct {
	CredentialDirs   []string `json:"credentialDirs"`
	HomeOnlyDirs     []string `json:"homeOnlyDirs"`
	CredentialFiles  []string `json:"credentialFiles"`
	CredentialNames  string   `json:"credentialNames"`
	OutboundVerdicts []struct {
		Path    string `json:"path"`
		Refused bool   `json:"refused"`
	} `json:"outboundVerdicts"`
}

func loadRuntimeLists(t *testing.T) runtimeLists {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "runtime-lists.json"))
	if err != nil {
		t.Fatal(err)
	}
	var l runtimeLists
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

// The floor is the runtimes' list. check-org.sh holds their lane.go to the
// fixture; this holds the module to it.
func TestTheFloorHoldsTheRuntimesList(t *testing.T) {
	l := loadRuntimeLists(t)
	for _, d := range l.CredentialDirs {
		if !slices.Contains(credentialDirs, d) {
			t.Errorf("credential directory %q of the runtimes is missing", d)
		}
	}
	for _, f := range l.CredentialFiles {
		if !slices.Contains(credentialFiles, f) {
			t.Errorf("credential file %q of the runtimes is missing", f)
		}
	}
	for _, d := range l.HomeOnlyDirs {
		if !homeOnlyDirs[d] {
			t.Errorf("home-only directory %q of the runtimes is not home-only here", d)
		}
	}
	if len(homeOnlyDirs) != len(l.HomeOnlyDirs) {
		t.Errorf("home-only directories differ: %v vs %v", homeOnlyDirs, l.HomeOnlyDirs)
	}
}

// Verdicts rather than set membership: the runtimes' name rule is a regular
// expression, which a list of places cannot "contain".
func TestTheOutboundPolicyGivesTheRuntimesVerdicts(t *testing.T) {
	out, err := Outbound(realTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range loadRuntimeLists(t).OutboundVerdicts {
		_, why := out.Check(v.Path)
		if got := why != ""; got != v.Refused {
			t.Errorf("Outbound.Check(%q): refused = %v (%s), want %v", v.Path, got, why, v.Refused)
		}
	}
}

// The operator's decision (2026-09-22): a copy of a credential file in an
// evidence collection is read on this machine, and not sent off it; the real
// one under home is neither.
func TestLocalReadsACopyThatOutboundWillNotSend(t *testing.T) {
	home := realTemp(t)
	write(t, filepath.Join(home, ".bash_history"))
	evidence := filepath.Join(realTemp(t), "evidence", "home", "bob", ".bash_history")
	write(t, evidence)
	local, err := Local(home)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Outbound(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, why := local.Check(evidence); why != "" {
		t.Errorf("Local refused the evidence copy: %s", why)
	}
	if _, why := out.Check(evidence); why == "" {
		t.Error("Outbound would send the evidence copy")
	}
	for _, p := range []Policy{local, out} {
		if _, why := p.Check(filepath.Join(home, ".bash_history")); why == "" {
			t.Error("the real ~/.bash_history passed")
		}
	}
}

func TestLocalRefusesEveryRealPlaceUnderHome(t *testing.T) {
	home := realTemp(t)
	local, err := Local(home)
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, d := range append(append([]string{}, credentialDirs...), agentControlDirs...) {
		rels = append(rels, d+"/x")
	}
	rels = append(rels, credentialFiles...)
	for _, rel := range rels {
		if _, why := local.Check(filepath.Join(home, filepath.FromSlash(rel))); why == "" {
			t.Errorf("Local passed ~/%s", rel)
		}
	}
	if _, why := local.Check(filepath.Join(home, "Downloads", "capture.pcapng")); why != "" {
		t.Errorf("an ordinary file under home was refused: %s", why)
	}
}

func TestEnvFilesAreRefusedAndTheirTemplatesAreNot(t *testing.T) {
	local, err := Local(realTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/srv/app/.env", "/srv/app/.env.production", "/srv/app/.ENV"} {
		if _, why := local.Check(p); why == "" {
			t.Errorf("Local passed %s", p)
		}
	}
	for _, p := range []string{"/srv/app/.env.example", "/srv/app/.env.sample", "/srv/app/.env.template", "/srv/app/.env.dist", "/srv/app/environment.csv"} {
		if _, why := local.Check(p); why != "" {
			t.Errorf("Local refused %s: %s", p, why)
		}
	}
}

// System locations refuse a work directory, not a file: reading /etc/hosts is
// not a leak.
func TestTheFilePoliciesLeaveSystemLocationsAlone(t *testing.T) {
	local, err := Local(realTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, why := local.Check("/etc/hosts"); why != "" {
		t.Errorf("Local refused /etc/hosts: %s", why)
	}
}

func TestAZeroPolicyRefusesEverything(t *testing.T) {
	if reason, _ := (Policy{}).Check("/srv/data/report.pdf"); reason != "unconfigured" {
		t.Errorf("zero Policy: %q", reason)
	}
}

func TestProtectedPlacesAreRefusedWithTheirOwnWords(t *testing.T) {
	own := filepath.Join(realTemp(t), "chrome-pilot-mcp")
	mkdir(t, filepath.Join(own, "profiles"))
	local, err := Local(realTemp(t), ServerDir(own, "which holds its browser profiles"))
	if err != nil {
		t.Fatal(err)
	}
	reason, why := local.Check(filepath.Join(own, "profiles", "Cookies"))
	if reason != "server_dir" || !strings.Contains(why, "which holds its browser profiles") {
		t.Errorf("got %q / %q", reason, why)
	}
}

// Two consumers promise no third-party dependency; this module keeps the
// promise for them.
func TestTheModuleRequiresNothing(t *testing.T) {
	b, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "require") {
			t.Fatalf("go.mod gained a dependency: %q", line)
		}
	}
}

// What only identity catches. A hard link to a floor file under another name
// elsewhere has neither the name nor a link to follow.
func TestAHardLinkToAFloorFileIsRefused(t *testing.T) {
	home := realTemp(t)
	netrc := filepath.Join(home, ".netrc")
	write(t, netrc)
	elsewhere := filepath.Join(realTemp(t), "notes.txt")
	if err := os.Link(netrc, elsewhere); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	if reason, _ := Check(floorOf(t, home), elsewhere); reason != "sensitive_path" {
		t.Errorf("a hard link to ~/.netrc: reason = %q, want sensitive_path", reason)
	}
}

// A darwin firmlink is not a symlink: /System/Volumes/Data/<path> is <path>,
// EvalSymlinks leaves the spelling alone, and only identity sees one directory.
// The file policies are where it matters — a work_dir spelled that way is
// already refused, by name, as lying in /System.
func TestAFirmlinkSpellingIsRefused(t *testing.T) {
	home := realTemp(t)
	mkdir(t, filepath.Join(home, ".ssh"))
	firm := filepath.Join("/System/Volumes/Data", home, ".ssh", "id_rsa")
	if _, err := os.Stat(filepath.Dir(firm)); err != nil {
		t.Skipf("no firmlinked data volume here: %v", err)
	}
	local, err := Local(home)
	if err != nil {
		t.Fatal(err)
	}
	if reason, _ := local.Check(firm); reason != "sensitive_path" {
		t.Errorf("Local.Check(%q) reason = %q, want sensitive_path", firm, reason)
	}
}
