package pathguard

import (
	"path/filepath"
	"strings"
)

// envTemplates are committed .env templates — examples, not secrets — as the
// runtimes already decided.
var envTemplates = map[string]bool{
	".env.example": true, ".env.sample": true, ".env.template": true, ".env.dist": true,
}

// EnvFile reports whether a file name is a .env file holding secrets: .env and
// .env.*, except the committed templates. Names are compared as a
// case-insensitive filesystem compares them (segKey).
func EnvFile(name string) bool {
	b := segKey(name)
	if foldedTemplates[b] {
		return false
	}
	return b == fold(".env") || strings.HasPrefix(b, fold(".env."))
}

var foldedTemplates = func() map[string]bool {
	m := map[string]bool{}
	for t := range envTemplates {
		m[fold(t)] = true
	}
	return m
}()

// SecretName reports whether a file name marks a secret wherever the file sits:
// a .env file, a private key's default name, a credentials file, or a service
// account key. Case is ignored.
func SecretName(name string) bool {
	if EnvFile(name) {
		return true
	}
	b := segKey(name)
	for _, s := range secretNames {
		if b == fold(s) {
			return true
		}
	}
	return strings.Contains(b, fold("service-account")) && strings.HasSuffix(b, fold(".json"))
}

var secretNames = []string{
	"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa",
	"credentials.json", ".credentials.json", "application_default_credentials.json",
}

// CredentialSegment returns the credential directory or file name that p
// passes through as a path segment — anywhere, not only under a home
// directory — or "". The names that also occur inside projects with another
// meaning (.claude, .gemini, .codex) are skipped. Names are compared as a
// case-insensitive filesystem compares them (segKey). A name may span several
// segments (.config/gcloud, .docker/config.json).
func CredentialSegment(p string) string {
	folded := foldPath(filepath.ToSlash(p))
	for _, d := range credentialDirs {
		if !homeOnlyDirs[d] && hasSegments(folded, foldPath(d)) {
			return d
		}
	}
	for _, f := range credentialFiles {
		if hasSegments(folded, foldPath(f)) {
			return f
		}
	}
	return ""
}

// hasSegments reports whether name occurs in p as whole segments.
func hasSegments(p, name string) bool {
	return p == name || strings.HasPrefix(p, name+"/") ||
		strings.HasSuffix(p, "/"+name) || strings.Contains(p, "/"+name+"/")
}
