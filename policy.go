package pathguard

import "path/filepath"

// Policy judges file paths. There are two, because what may be read or written
// on this machine and what may be sent off it are different questions: a key
// that has left the machine cannot be taken back, while a copy of one inside an
// incident-response collection is exactly what an analyst needs to read.
//
// The zero Policy refuses everything: a check that was never set up must not
// pass paths.
type Policy struct {
	places   []Place
	outbound bool
	built    bool
}

// Local is the policy for reading and writing on this machine. It refuses the
// real credential and agent-control places under home, compared by identity
// and by name, .env files anywhere except the committed templates, and the
// protected places given. A file of the same name elsewhere — a copy in an
// evidence collection, a project's own .npmrc — passes.
func Local(home string, protected ...Place) (Policy, error) {
	return newPolicy(home, false, protected)
}

// Outbound is the policy for a file that leaves the machine. It refuses what
// Local refuses, and also a credential directory or file name as a path
// segment anywhere (CredentialSegment) and a secret file name anywhere
// (SecretName).
func Outbound(home string, protected ...Place) (Policy, error) {
	return newPolicy(home, true, protected)
}

func newPolicy(home string, outbound bool, protected []Place) (Policy, error) {
	floor, err := Floor(home)
	if err != nil {
		return Policy{}, err
	}
	var places []Place
	for _, p := range floor {
		// System locations refuse a work directory, not a file: reading
		// /etc/hosts is not a leak, and writes stay under the work directory.
		if p.Kind != System {
			places = append(places, p)
		}
	}
	places = append(places, protected...)
	return Policy{places: places, outbound: outbound, built: true}, nil
}

// Check reports why any of paths may not be touched under this policy — its
// reason and sentence — or two empty strings. Pass every spelling you have:
// the path as the caller gave it and its resolved form.
func (p Policy) Check(paths ...string) (reason, why string) {
	if !p.built {
		return "unconfigured", "the path check was not set up (pathguard.Local or pathguard.Outbound)"
	}
	if reason, why := Check(p.places, paths...); why != "" {
		return reason, why
	}
	views, _ := viewsOf(paths)
	for _, v := range views {
		if EnvFile(filepath.Base(v.path)) {
			return "sensitive_path", "a .env file holds credentials"
		}
	}
	if !p.outbound {
		return "", ""
	}
	for _, v := range views {
		if SecretName(filepath.Base(v.path)) {
			return "sensitive_path", "its name marks it as a secret, and a file named like this is not sent off the machine"
		}
		if n := CredentialSegment(v.path); n != "" {
			return "sensitive_path", "it lies in a " + n + " location, which is not sent off the machine"
		}
	}
	return "", ""
}
