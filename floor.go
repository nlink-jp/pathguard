package pathguard

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrNoHome is returned when the home directory is not known. The credential
// floor is relative to it, so without it nothing can be checked, and the
// caller must refuse rather than pass everything — which is what the copies
// this module replaced did.
var ErrNoHome = errors.New("the home directory cannot be determined, so the credential floor cannot be applied")

// The one list. Its credential part is gem-agent's and lagent's
// (internal/sandbox/lane.go), which has had two security reviews;
// testdata/runtime-lists.json holds a copy that the tests and check-org.sh
// hold both sides to.
var (
	// systemExact may not be a work directory themselves, but a directory
	// below them may: /private/var holds every per-user temporary directory.
	systemExact = []string{"/", "/private/var"}
	// systemTrees may not hold a work directory anywhere below them. /etc and
	// /private/etc are both listed (/etc is a link on darwin, a directory on
	// Linux); identity makes the duplicate free.
	systemTrees = []string{"/bin", "/sbin", "/usr", "/etc", "/private/etc", "/System", "/Library", "/Applications"}
	// credentialDirs are home-relative directories whose contents are secrets.
	credentialDirs = []string{
		".ssh", ".aws", ".kube", ".gnupg", ".config/gcloud", ".config/gh",
		".gemini", ".codex", ".claude", ".azure", ".terraform.d", "Library/Keychains",
		".config/mcp-bridge",
	}
	// homeOnlyDirs are credentialDirs whose names also occur inside projects
	// with another meaning (a project's .claude/ holds its skills): the
	// by-name rule of the outbound policy skips them, and only the real ones
	// under the home directory are protected.
	homeOnlyDirs = map[string]bool{".claude": true, ".gemini": true, ".codex": true}
	// credentialFiles are home-relative files that are secrets.
	credentialFiles = []string{
		".docker/config.json", ".git-credentials", ".bash_history", ".zsh_history",
		".netrc", ".npmrc", ".pypirc", ".vault-token", ".claude.json",
	}
	// agentControlDirs steer an agent of this organization; a server must not
	// be the way its configuration is read or rewritten.
	agentControlDirs = []string{".config/gem-agent", ".config/lagent"}
)

// Floor builds the one list of places for a home directory: the system
// locations and the home directory itself, the credential directories and
// files, and the agent-control directories. An empty home is ErrNoHome.
func Floor(home string) ([]Place, error) {
	if strings.TrimSpace(home) == "" {
		return nil, ErrNoHome
	}
	home = filepath.Clean(home)
	var out []Place
	for _, d := range systemExact {
		out = append(out, Place{Path: d, Exact: true, Kind: System, Reason: "system_dir", Why: "it is a system directory"})
	}
	out = append(out, Place{Path: home, Exact: true, Kind: System, Reason: "home_dir",
		Why: "it is the home directory itself; pass a directory inside it"})
	for _, d := range systemTrees {
		out = append(out, Place{Path: d, Kind: System, Reason: "system_dir", Why: "it is inside the system directory " + d})
	}
	for _, rel := range credentialDirs {
		out = append(out, homePlace(home, rel, Credential))
	}
	for _, rel := range credentialFiles {
		out = append(out, homePlace(home, rel, Credential))
	}
	for _, rel := range agentControlDirs {
		out = append(out, homePlace(home, rel, AgentControl))
	}
	return out, nil
}

func homePlace(home, rel string, kind Kind) Place {
	return Place{
		Path:   filepath.Join(home, filepath.FromSlash(rel)),
		Kind:   kind,
		Reason: "sensitive_path",
		Why:    "~/" + rel + " holds credentials or agent control files",
	}
}
