// Package identifier holds the 'directories' identifier backend. It embeds the lico
// LDAP backend (idm stays the directory for identity, groups, claims) and only
// overrides Logon to delegate the PASSWORD check to an external directory for
// users whose auth directory points there. See OPENCORE_DIRECTORY.md §6/§8.
package identifier

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	ldap "github.com/go-ldap/ldap/v3"
	"github.com/sirupsen/logrus"

	"github.com/libregraph/lico/identifier/backends"
	licoldap "github.com/libregraph/lico/identifier/backends/ldap"

	"github.com/opencloud-eu/opencloud/services/idp/pkg/backends/directories"
)

const directoriesIdentifierBackendName = "identifier-directories"

// Backend delegates password verification per user while keeping idm as
// the authoritative source for identity, groups and claims.
type Backend struct {
	*licoldap.LDAPIdentifierBackend // identity/claims/session: unchanged idm behaviour

	logger logrus.FieldLogger
	dirs   *directories.DirectorySet

	// parameters to look the user up in idm (DN + auth-directory attribute) and
	// to perform the internal bind. Mirror of what the embedded backend uses.
	idmURI      string
	idmBindDN   string
	idmBindPW   string
	idmBaseDN   string
	idmScope    int
	idmFilter   string // lico's composed user filter, e.g. (&(objectclass=..)(!(enabled=FALSE)))
	loginAttr   string
	authDirAttr string
	idmTLS      *tls.Config
}

// Config carries everything the directories backend needs on top of the embedded
// lico LDAP backend.
type Config struct {
	LDAP   *licoldap.LDAPIdentifierBackend
	Logger logrus.FieldLogger
	Dirs   *directories.DirectorySet

	IDMURI      string
	IDMBindDN   string
	IDMBindPW   string
	IDMBaseDN   string
	IDMScope    int
	IDMFilter   string
	LoginAttr   string
	AuthDirAttr string
	IDMTLS      *tls.Config
}

// NewBackend builds a directories Backend from the given config.
func NewBackend(c Config) (*Backend, error) {
	if c.LDAP == nil {
		return nil, fmt.Errorf("directories backend: embedded LDAP backend is nil")
	}
	if c.Dirs == nil {
		c.Dirs, _ = directories.ParseDirectories("")
	}
	if c.LoginAttr == "" {
		return nil, fmt.Errorf("directories backend: login attribute is empty")
	}
	if c.AuthDirAttr == "" {
		c.AuthDirAttr = "openCoreAuthDirectory"
	}
	if c.Logger == nil {
		c.Logger = logrus.New()
	}
	return &Backend{
		LDAPIdentifierBackend: c.LDAP,
		logger:                c.Logger,
		dirs:                  c.Dirs,
		idmURI:                c.IDMURI,
		idmBindDN:             c.IDMBindDN,
		idmBindPW:             c.IDMBindPW,
		idmBaseDN:             c.IDMBaseDN,
		idmScope:              c.IDMScope,
		idmFilter:             c.IDMFilter,
		loginAttr:             c.LoginAttr,
		authDirAttr:           c.AuthDirAttr,
		idmTLS:                c.IDMTLS,
	}, nil
}

// Name implements the Backend interface.
func (b *Backend) Name() string { return directoriesIdentifierBackendName }

// Logon verifies the password against idm (internal) or the user's external
// directory, while identity and claims always come from idm.
func (b *Backend) Logon(ctx context.Context, audience, username, password string) (bool, *string, *string, backends.UserFromBackend, error) {
	authDir, found, err := b.lookupAuthDirectory(username)
	if err != nil {
		return false, nil, nil, nil, err
	}
	if !found {
		return false, nil, nil, nil, nil // unknown user
	}

	if directories.IsInternal(authDir) {
		// internal: full reuse of the unchanged idm backend (search + bind + claims).
		return b.LDAPIdentifierBackend.Logon(ctx, audience, username, password)
	}

	dir, ok := b.dirs.Pick(authDir)
	if !ok {
		return false, nil, nil, nil, fmt.Errorf("directories: user %q points to unknown auth directory %q", username, authDir)
	}

	okBind, err := b.bindExternal(dir, username, password)
	if err != nil {
		return false, nil, nil, nil, err
	}
	if !okBind {
		return false, nil, nil, nil, nil // wrong password
	}

	// Password verified externally; identity, UUID, groups and claims from idm.
	user, err := b.LDAPIdentifierBackend.ResolveUserByUsername(ctx, username)
	if err != nil {
		return false, nil, nil, nil, fmt.Errorf("directories: external logon ok but idm resolve failed: %w", err)
	}
	if user == nil {
		return false, nil, nil, nil, nil
	}
	userID := user.Subject()
	b.logger.WithFields(logrus.Fields{"username": username, "directory": dir.Name}).Debugln("directories external logon")
	return true, &userID, nil, user, nil
}

// lookupAuthDirectory finds the user in idm and returns its auth-directory value.
func (b *Backend) lookupAuthDirectory(username string) (string, bool, error) {
	conn, err := dial(b.idmURI, b.idmTLS)
	if err != nil {
		return "", false, fmt.Errorf("directories: idm connect: %w", err)
	}
	defer conn.Close()

	if b.idmBindDN != "" {
		if err := conn.Bind(b.idmBindDN, b.idmBindPW); err != nil {
			return "", false, fmt.Errorf("directories: idm service bind: %w", err)
		}
	}

	filter := fmt.Sprintf("(%s=%s)", b.loginAttr, ldap.EscapeFilter(username))
	if b.idmFilter != "" {
		filter = fmt.Sprintf("(&%s%s)", b.idmFilter, filter)
	}
	req := ldap.NewSearchRequest(
		b.idmBaseDN, b.idmScope, ldap.NeverDerefAliases, 0, 0, false,
		filter, []string{b.authDirAttr, b.loginAttr}, nil,
	)
	res, err := conn.Search(req)
	if err != nil {
		return "", false, fmt.Errorf("directories: idm search: %w", err)
	}
	if len(res.Entries) == 0 {
		return "", false, nil
	}
	entry := res.Entries[0]
	if !strings.EqualFold(entry.GetAttributeValue(b.loginAttr), username) {
		return "", false, nil
	}
	return strings.TrimSpace(entry.GetAttributeValue(b.authDirAttr)), true, nil
}

// bindExternal tries the directory's URIs in order and binds as the user via the
// dn_template. A wrong password stops immediately; a connection error fails over
// to the next URI (E8.6a, §6 Baustein 2).
func (b *Backend) bindExternal(dir directories.Directory, username, password string) (bool, error) {
	if password == "" {
		return false, nil // never allow an unauthenticated (anonymous) bind
	}
	bindDN := dir.BindDN(username)

	tlsCfg, err := dirTLS(dir)
	if err != nil {
		return false, err
	}

	var lastErr error
	for _, uri := range dir.URIs {
		conn, derr := dial(uri, tlsCfg)
		if derr != nil {
			lastErr = derr
			continue // connection error -> failover to next URI
		}
		berr := conn.Bind(bindDN, password)
		conn.Close()
		if berr == nil {
			return true, nil
		}
		if ldap.IsErrorWithCode(berr, ldap.LDAPResultInvalidCredentials) {
			return false, nil // wrong password: do NOT fail over
		}
		lastErr = berr // other error -> try next URI
	}
	if lastErr != nil {
		return false, fmt.Errorf("directories: external directory %q unreachable: %w", dir.Name, lastErr)
	}
	return false, nil
}

func dial(uri string, tlsCfg *tls.Config) (*ldap.Conn, error) {
	opts := []ldap.DialOpt{ldap.DialWithDialer(&net.Dialer{Timeout: 10 * time.Second})}
	if tlsCfg != nil {
		opts = append(opts, ldap.DialWithTLSConfig(tlsCfg))
	}
	return ldap.DialURL(uri, opts...)
}

func dirTLS(dir directories.Directory) (*tls.Config, error) {
	if dir.CACertFile == "" {
		return nil, nil
	}
	pem, err := os.ReadFile(dir.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("directories: read CA %s: %w", dir.CACertFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("directories: cannot parse CA %s", dir.CACertFile)
	}
	return &tls.Config{RootCAs: pool}, nil
}
