package identity

import (
	"context"

	"github.com/go-ldap/ldap/v3"

	"github.com/opencloud-eu/opencloud/services/graph/pkg/errorcode"
)

// GetUserAuthDirectory returns the name of the auth directory that authenticates
// the given user. An empty string means the built-in idm (internal). This is an
// OpenCore extension used by the "directories" identity manager; see
// opendock/OPENCORE_DIRECTORY.md.
func (i *LDAP) GetUserAuthDirectory(ctx context.Context, nameOrID string) (string, error) {
	logger := i.logger.SubloggerWithRequestID(ctx)
	logger.Debug().Str("nameOrID", nameOrID).Msg("GetUserAuthDirectory")

	e, err := i.getLDAPUserByNameOrID(nameOrID)
	if err != nil {
		return "", err
	}

	// getLDAPUserByNameOrID does not request the OpenCore attribute, so read it
	// with a targeted base-scope search on the user DN.
	attr := i.userAttributeMap.authDirectory
	sr := ldap.NewSearchRequest(
		e.DN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=*)", []string{attr}, nil,
	)
	res, err := i.conn.Search(sr)
	if err != nil {
		return "", errorcode.New(errorcode.GeneralException, err.Error())
	}
	if len(res.Entries) == 0 {
		return "", nil
	}
	return res.Entries[0].GetEqualFoldAttributeValue(attr), nil
}

// SetUserAuthDirectory sets (or, for an empty value, removes) the per-user auth
// directory attribute. An empty value means internal (idm). OpenCore extension.
func (i *LDAP) SetUserAuthDirectory(ctx context.Context, nameOrID, directory string) error {
	logger := i.logger.SubloggerWithRequestID(ctx)
	logger.Debug().Str("nameOrID", nameOrID).Str("directory", directory).Msg("SetUserAuthDirectory")

	e, err := i.getLDAPUserByNameOrID(nameOrID)
	if err != nil {
		return err
	}

	attr := i.userAttributeMap.authDirectory
	mr := ldap.NewModifyRequest(e.DN, nil)
	if directory == "" {
		// internal: remove the attribute (Replace with no values deletes it).
		mr.Replace(attr, []string{})
	} else {
		mr.Replace(attr, []string{directory})
	}
	if err := i.conn.Modify(mr); err != nil {
		return errorcode.New(errorcode.GeneralException, err.Error())
	}
	return nil
}
