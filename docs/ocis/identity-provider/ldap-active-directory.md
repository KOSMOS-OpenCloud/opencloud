---
title: "LDAP - Active Directory"
date: 2023-05-03T00:00:00+00:00
weight: 20
geekdocRepo: https://github.com/owncloud/ocis
geekdocEditPath: edit/master/docs/ocis/identity-provider
geekdocFilePath: ldap-active-directory.md
geekdocCollapseSection: true
---

## Overview

oCIS can be configured using Active Directory as identity provider.

## Configuration Example

This configuration is an _example_ for using Samba4 AD as well as a Windows Server 2022 as the LDAP backend for oCIS. It is intended as guideline and first starting point.

```text
OC_LDAP_URI=ldaps://xxxxxxxxx
OC_LDAP_INSECURE="true"
OC_LDAP_BIND_DN="cn=administrator,cn=users,xxxxxxxxxx"
OC_LDAP_BIND_PASSWORD=xxxxxxx
OC_LDAP_DISABLE_USER_MECHANISM="none"
OC_LDAP_GROUP_BASE_DN="dc=owncloud,dc=test"
OC_LDAP_GROUP_OBJECTCLASS="group"
OC_LDAP_GROUP_SCHEMA_ID="objectGUID"
OC_LDAP_GROUP_SCHEMA_ID_IS_OCTETSTRING="true"
OC_LDAP_GROUP_SCHEMA_GROUPNAME="cn"
OC_LDAP_USER_BASE_DN="dc=owncloud,dc=test"
OC_LDAP_USER_OBJECTCLASS="user"
OC_LDAP_USER_SCHEMA_ID="objectGUID"
OC_LDAP_USER_SCHEMA_ID_IS_OCTETSTRING="true"
OC_LDAP_USER_SCHEMA_USERNAME="sAMAccountName"
OC_LDAP_LOGIN_ATTRIBUTES="sAMAccountName"
IDP_LDAP_LOGIN_ATTRIBUTE="sAMAccountName"
IDP_LDAP_UUID_ATTRIBUTE="objectGUID"
IDP_LDAP_UUID_ATTRIBUTE_TYPE=binary
GRAPH_LDAP_SERVER_WRITE_ENABLED="false"
OC_EXCLUDE_RUN_SERVICES=idm
OC_ADMIN_USER_ID="<objectGUID-value-of-the-default-admin-user>"
```
