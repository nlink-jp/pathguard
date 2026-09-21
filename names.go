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
// .env.*, except the committed templates. Case is ignored.
func EnvFile(name string) bool {
	b := strings.ToLower(name)
	if envTemplates[b] {
		return false
	}
	return b == ".env" || strings.HasPrefix(b, ".env.")
}

// SecretName reports whether a file name marks a secret wherever the file sits:
// a .env file, a private key's default name, a credentials file, or a service
// account key. Case is ignored.
func SecretName(name string) bool {
	b := strings.ToLower(name)
	switch {
	case EnvFile(b):
		return true
	case b == "id_rsa", b == "id_ed25519", b == "id_ecdsa", b == "id_dsa":
		return true
	case b == "credentials.json", b == ".credentials.json", b == "application_default_credentials.json":
		return true
	case strings.Contains(b, "service-account") && strings.HasSuffix(b, ".json"):
		return true
	}
	return false
}

// CredentialSegment returns the credential directory or file name that p
// passes through as a path segment — anywhere, not only under a home
// directory — or "". The names that also occur inside projects with another
// meaning (.claude, .gemini, .codex) are skipped. Case is ignored. A name may
// span several segments (.config/gcloud, .docker/config.json).
func CredentialSegment(p string) string {
	lower := strings.ToLower(filepath.ToSlash(p))
	for _, d := range credentialDirs {
		if !homeOnlyDirs[d] && hasSegments(lower, strings.ToLower(d)) {
			return d
		}
	}
	for _, f := range credentialFiles {
		if hasSegments(lower, strings.ToLower(f)) {
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
