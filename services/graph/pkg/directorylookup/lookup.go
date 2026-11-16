// Package directorylookup performs a read-only existence check of a login in an
// external authentication directory for the OpenCore 'directories' identity
// manager. It is intentionally decoupled from the idp backend: the graph service
// uses it to confirm, before an admin assigns a user to an external directory,
// that the account actually exists there (E9). The directory is queried
// anonymously and NO attributes are requested, so no credential material is ever
// read. See opendock/OPENCORE_DIRECTORY.md.
package directorylookup

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/opencloud-eu/opencloud/pkg/shared"
)

// dialTimeout bounds a single connection attempt to one directory URI.
const dialTimeout = 10 * time.Second

// Lookup reports whether login exists in the external directory. It binds
// anonymously and does a base-scoped existence search on the DN built from the
// directory's dn_template, requesting no attributes ("1.1"). The directory's
// URIs are tried in order (failover). A nil error with false means the server
// was reachable and the DN does not exist; a non-nil error means no directory
// URI could be reached and the result is unknown (callers must not treat that as
// "does not exist").
func Lookup(dir shared.Directory, login string) (bool, error) {
	bindDN := buildBindDN(dir.DNTemplate, login)

	tlsCfg, err := directoryTLS(dir)
	if err != nil {
		return false, err
	}

	req := ldap.NewSearchRequest(
		bindDN,
		ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 10, false,
		"(objectClass=*)",
		[]string{"1.1"}, // "1.1" = request no attributes, only existence
		nil,
	)

	var lastErr error
	for _, uri := range dir.URIs {
		conn, derr := dial(uri, tlsCfg)
		if derr != nil {
			lastErr = derr
			continue // connection error -> failover to next URI
		}
		// No Bind() call: the search runs as an anonymous client.
		res, serr := conn.Search(req)
		conn.Close()
		if serr == nil {
			return len(res.Entries) > 0, nil
		}
		if ldap.IsErrorWithCode(serr, ldap.LDAPResultNoSuchObject) {
			return false, nil // reachable server, DN does not exist: definitive "no"
		}
		lastErr = serr // other error -> try next URI
	}
	if lastErr != nil {
		return false, fmt.Errorf("external directory %q unreachable: %w", dir.Name, lastErr)
	}
	return false, nil
}

// buildBindDN fills the dn_template with the login. Same construction as the idp
// direct-bind path: the login is escaped for DN context and no search filter is
// built from it, so there is no injection surface.
func buildBindDN(dnTemplate, login string) string {
	return strings.ReplaceAll(dnTemplate, "%s", ldap.EscapeDN(login))
}

func dial(uri string, tlsCfg *tls.Config) (*ldap.Conn, error) {
	opts := []ldap.DialOpt{ldap.DialWithDialer(&net.Dialer{Timeout: dialTimeout})}
	if tlsCfg != nil {
		opts = append(opts, ldap.DialWithTLSConfig(tlsCfg))
	}
	return ldap.DialURL(uri, opts...)
}

func directoryTLS(dir shared.Directory) (*tls.Config, error) {
	if dir.CACertFile == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(dir.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("read CA %s: %w", dir.CACertFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("cannot parse CA %s", dir.CACertFile)
	}
	return &tls.Config{RootCAs: pool}, nil
}
