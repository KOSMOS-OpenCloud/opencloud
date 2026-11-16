// Package directories holds the 'directories' identity-manager backend for the IdP: it keeps
// idm as the single directory for all users and delegates ONLY the password
// check to an external LDAP for users whose auth directory points there.
// See opendock/OPENCORE_DIRECTORY.md (E1-E11, §6, §8).
package directories

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// InternalDirectory is the reserved name of the built-in idm directory. A user
// whose auth directory is empty or equals this name is authenticated against idm.
const InternalDirectory = "internal"

// Directory describes one external authentication directory (E6/E8.6a).
type Directory struct {
	Name       string   `json:"name"`
	Label      string   `json:"label"`
	URIs       []string `json:"uris"`        // >= 1, tried in order (failover)
	DNTemplate string   `json:"dn_template"` // e.g. "uid=%s,ou=Users,dc=nugmbh,dc=p,dc=nu"
	CACertFile string   `json:"cacert_file"` // optional, for ldaps
}

// DirectorySet is the configured set of external directories, keyed by lower-case name.
type DirectorySet struct {
	byName map[string]Directory
}

// ParseDirectories parses the JSON array handed over through the environment.
// An entry named "internal" is ignored: the built-in idm is always implicit.
func ParseDirectories(jsonBlob string) (*DirectorySet, error) {
	set := &DirectorySet{byName: map[string]Directory{}}
	blob := strings.TrimSpace(jsonBlob)
	if blob == "" {
		return set, nil
	}
	var list []Directory
	if err := json.Unmarshal([]byte(blob), &list); err != nil {
		return nil, fmt.Errorf("directories config: invalid JSON: %w", err)
	}
	for _, dir := range list {
		if strings.TrimSpace(dir.Name) == "" {
			return nil, fmt.Errorf("directory without name")
		}
		if IsInternal(dir.Name) {
			continue // internal is implicit, never an external entry
		}
		if dir.DNTemplate == "" || !strings.Contains(dir.DNTemplate, "%s") {
			return nil, fmt.Errorf("directory %q: dn_template must be set and contain %%s", dir.Name)
		}
		if len(dir.URIs) == 0 {
			return nil, fmt.Errorf("directory %q: at least one uri required", dir.Name)
		}
		key := strings.ToLower(strings.TrimSpace(dir.Name))
		if _, dup := set.byName[key]; dup {
			return nil, fmt.Errorf("directory %q: duplicate name", dir.Name)
		}
		set.byName[key] = dir
	}
	return set, nil
}

// IsInternal reports whether an auth-directory value means the built-in idm.
func IsInternal(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	return n == "" || n == InternalDirectory
}

// Pick returns the external directory for the given name, or false if unknown.
func (s *DirectorySet) Pick(name string) (Directory, bool) {
	dir, ok := s.byName[strings.ToLower(strings.TrimSpace(name))]
	return dir, ok
}

// Names returns the configured external directory names (unordered).
func (s *DirectorySet) Names() []string {
	names := make([]string, 0, len(s.byName))
	for _, dir := range s.byName {
		names = append(names, dir.Name)
	}
	return names
}

// BindDN builds the external bind DN for a username from the dn_template. The
// username is escaped for DN context (E4: direct bind, no search, no injection).
func (d Directory) BindDN(username string) string {
	return strings.ReplaceAll(d.DNTemplate, "%s", ldap.EscapeDN(username))
}
