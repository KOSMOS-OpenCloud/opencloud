// Package bootstrap registers the 'directories' identity manager with lico. It sets up
// the embedded lico LDAP backend against idm exactly as the stock "ldap" backend
// does, and wraps it so Logon can delegate the password check to an external
// directory per user. See opendock/OPENCORE_DIRECTORY.md §6/§8.
package bootstrap

import (
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	goldap "github.com/go-ldap/ldap/v3"
	"github.com/libregraph/lico/bootstrap"
	"github.com/libregraph/lico/identifier"
	licoldap "github.com/libregraph/lico/identifier/backends/ldap"
	"github.com/libregraph/lico/identity"
	"github.com/libregraph/lico/identity/managers"

	"github.com/opencloud-eu/opencloud/services/idp/pkg/backends/directories"
	kidentifier "github.com/opencloud-eu/opencloud/services/idp/pkg/backends/directories/identifier"
	"github.com/opencloud-eu/opencloud/services/idp/pkg/backends/directories/keystore"
)

const identityManagerName = "directories"

// Register registers the 'directories' identity manager.
func Register() error {
	return bootstrap.RegisterIdentityManager(identityManagerName, NewIdentityManager)
}

// MustRegister registers the 'directories' identity manager and panics on error.
func MustRegister() {
	if err := Register(); err != nil {
		panic(err)
	}
}

func scopeToInt(scope string) int {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "base":
		return goldap.ScopeBaseObject
	case "one":
		return goldap.ScopeSingleLevel
	default:
		return goldap.ScopeWholeSubtree
	}
}

// NewIdentityManager produces a 'directories' backed identity manager for the idp.
func NewIdentityManager(bs bootstrap.Bootstrap) (identity.Manager, error) {
	config := bs.Config()
	logger := config.Config.Logger

	if config.AuthorizationEndpointURI.String() != "" {
		return nil, fmt.Errorf("directories backend is incompatible with authorization-endpoint-uri parameter")
	}
	config.AuthorizationEndpointURI.Path = bs.MakeURIPath(bootstrap.APITypeSignin, "/identifier/_/authorize")

	if config.EndSessionEndpointURI.String() != "" {
		return nil, fmt.Errorf("directories backend is incompatible with endsession-endpoint-uri parameter")
	}
	config.EndSessionEndpointURI.Path = bs.MakeURIPath(bootstrap.APITypeSignin, "/identifier/_/endsession")

	if config.SignInFormURI.EscapedPath() == "" {
		config.SignInFormURI.Path = bs.MakeURIPath(bootstrap.APITypeSignin, "/identifier")
	}
	if config.SignedOutURI.EscapedPath() == "" {
		config.SignedOutURI.Path = bs.MakeURIPath(bootstrap.APITypeSignin, "/goodbye")
	}

	// Default LDAP attribute mappings (same as the stock ldap backend).
	attributeMapping := map[string]string{
		licoldap.AttributeLogin:                        os.Getenv("LDAP_LOGIN_ATTRIBUTE"),
		licoldap.AttributeEmail:                        os.Getenv("LDAP_EMAIL_ATTRIBUTE"),
		licoldap.AttributeName:                         os.Getenv("LDAP_NAME_ATTRIBUTE"),
		licoldap.AttributeFamilyName:                   os.Getenv("LDAP_FAMILY_NAME_ATTRIBUTE"),
		licoldap.AttributeGivenName:                    os.Getenv("LDAP_GIVEN_NAME_ATTRIBUTE"),
		licoldap.AttributeUUID:                         os.Getenv("LDAP_UUID_ATTRIBUTE"),
		fmt.Sprintf("%s_type", licoldap.AttributeUUID): os.Getenv("LDAP_UUID_ATTRIBUTE_TYPE"),
	}
	if numericUIDAttribute := os.Getenv("LDAP_UIDNUMBER_ATTRIBUTE"); numericUIDAttribute != "" {
		attributeMapping[licoldap.AttributeNumericUID] = numericUIDAttribute
	}
	var subMapping []string
	if subMappingString := os.Getenv("LDAP_SUB_ATTRIBUTES"); subMappingString != "" {
		subMapping = strings.Split(subMappingString, " ")
	}

	tlsConfig := config.TLSClientConfig.Clone()
	if caCertFile := os.Getenv("LDAP_TLS_CACERT"); caCertFile != "" {
		pemBytes, err := os.ReadFile(caCertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate(s) from '%s': %w", caCertFile, err)
		}
		rpool, _ := x509.SystemCertPool()
		if rpool == nil {
			rpool = x509.NewCertPool()
		}
		if !rpool.AppendCertsFromPEM(pemBytes) {
			return nil, fmt.Errorf("failed to append CA certificate(s) from '%s' to pool", caCertFile)
		}
		tlsConfig.RootCAs = rpool
	}

	// The embedded lico LDAP backend against idm (identity, groups, claims).
	embedded, err := licoldap.NewLDAPIdentifierBackend(
		config.Config,
		tlsConfig,
		os.Getenv("LDAP_URI"),
		os.Getenv("LDAP_BINDDN"),
		os.Getenv("LDAP_BINDPW"),
		os.Getenv("LDAP_BASEDN"),
		os.Getenv("LDAP_SCOPE"),
		os.Getenv("LDAP_FILTER"),
		subMapping,
		attributeMapping,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedded ldap backend: %w", err)
	}

	dirs, err := directories.ParseDirectories(os.Getenv("DIRECTORIES_JSON"))
	if err != nil {
		return nil, err
	}
	authDirAttr := os.Getenv("DIRECTORIES_AUTH_ATTRIBUTE")
	if authDirAttr == "" {
		authDirAttr = "openCoreAuthDirectory"
	}

	// Optional per-user key store (opendock/OPENCORE_KEYSTORE.md §10). Without a
	// path it is disabled and Logon behaves exactly as before.
	var keystoreHook *keystore.Hook
	userKeystore, err := keystore.Open(os.Getenv("KEYSTORE_PATH"), os.Getenv("KEYSTORE_SERVICE_KEYS"), os.Getenv("KEYSTORE_SESSION_TTL"))
	if err != nil {
		return nil, err
	}
	if userKeystore != nil {
		keystoreHook = keystore.NewHook(userKeystore, logger)
		logger.Infoln("directories backend: key store enabled")
	}

	identifierBackend, err := kidentifier.NewBackend(kidentifier.Config{
		LDAP:        embedded,
		Logger:      logger,
		Dirs:        dirs,
		Keystore:    keystoreHook,
		IDMURI:      os.Getenv("LDAP_URI"),
		IDMBindDN:   os.Getenv("LDAP_BINDDN"),
		IDMBindPW:   os.Getenv("LDAP_BINDPW"),
		IDMBaseDN:   os.Getenv("LDAP_BASEDN"),
		IDMScope:    scopeToInt(os.Getenv("LDAP_SCOPE")),
		IDMFilter:   os.Getenv("LDAP_FILTER"),
		LoginAttr:   os.Getenv("LDAP_LOGIN_ATTRIBUTE"),
		AuthDirAttr: authDirAttr,
		IDMTLS:      tlsConfig,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create directories backend: %w", err)
	}

	fullAuthorizationEndpointURL := bootstrap.WithSchemeAndHost(config.AuthorizationEndpointURI, config.IssuerIdentifierURI)
	fullSignInFormURL := bootstrap.WithSchemeAndHost(config.SignInFormURI, config.IssuerIdentifierURI)
	fullSignedOutEndpointURL := bootstrap.WithSchemeAndHost(config.SignedOutURI, config.IssuerIdentifierURI)

	activeIdentifier, err := identifier.NewIdentifier(&identifier.Config{
		Config: config.Config,

		BaseURI:        config.IssuerIdentifierURI,
		PathPrefix:     bs.MakeURIPath(bootstrap.APITypeSignin, ""),
		StaticFolder:   config.IdentifierClientPath,
		ScopesConf:     config.IdentifierScopesConf,
		WebAppDisabled: config.IdentifierClientDisabled,

		LogonCookieName:     "__Secure-KKT",
		LogonCookieSameSite: config.CookieSameSite,

		ConsentCookieSameSite: config.CookieSameSite,

		StateCookieSameSite: config.CookieSameSite,

		AuthorizationEndpointURI: fullAuthorizationEndpointURL,
		SignedOutEndpointURI:     fullSignedOutEndpointURL,

		DefaultBannerLogo:        config.IdentifierDefaultBannerLogo,
		DefaultSignInPageText:    config.IdentifierDefaultSignInPageText,
		DefaultSignInPageLogoURI: config.IdentifierDefaultLogoTargetURI,
		DefaultUsernameHintText:  config.IdentifierDefaultUsernameHintText,
		UILocales:                config.IdentifierUILocales,

		Backend: identifierBackend,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create identifier: %w", err)
	}
	if err := activeIdentifier.SetKey(config.EncryptionSecret); err != nil {
		return nil, fmt.Errorf("invalid --encryption-secret parameter value for identifier: %w", err)
	}

	identityManagerConfig := &identity.Config{
		SignInFormURI:   fullSignInFormURL,
		SignedOutURI:    fullSignedOutEndpointURL,
		Logger:          logger,
		ScopesSupported: config.Config.AllowedScopes,
	}

	identifierIdentityManager := managers.NewIdentifierIdentityManager(identityManagerConfig, activeIdentifier)
	logger.Infoln("using directories identifier backed identity manager")

	return identifierIdentityManager, nil
}
