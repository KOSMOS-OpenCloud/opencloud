---
title: "1. Simple Multi-Tenancy using a single OpenCloud Instance"
---

* Status: proposed
* Deciders: [@micbar @butonic @dragotin @rhafer]
* Date: 2025-05-20

Technical Story: https://github.com/opencloud-eu/opencloud/issues/877

## Context and Problem Statement

To reduce resource usage and cost service providers want a single OpenCloud
instance to host multiple tenants. Members of the same tenant should be able to
only see each other when trying to share resources. A user can only be a member
of a single tenant. Moving a user from one tenant to another is not supported.
For the scope of this ADR we assume the tenants are overall rather small
(sometimes just a single user, often less than 10). There is just a single IDP
with a single "realm". Also there is no need to support per tenant groups of
users for now.

## Decision Drivers

* Low Resource Overhead: The solution should not require a lot of additional
  resources (CPU, Memory, Storage) on the OpenCloud instance.
* Implementation effort: The solution should be easy to implement and maintain.
* Security: The solution should prevent users from seeing or accessing anything
  from other tenants.

## Considered Options

The user <-> tenant id relation in maintained by the OIDC IDP and the tenant id
will be available as a claim in the OIDC tokens. A tenant id is assigned to
every user.

* Option 1: Users are autoprovisioned on the 1st login. The value of the tenant id
  will be persisted as a new part of the CS3 UserId. Everywhere the UserID is used
  the tenant id is also available. This allows to implement more sophisticated checks e.g.
  on permission grants and to a certain extend during share creation.
* Option 2: Users are autoprovisioned on the 1st login. The value of the tenant id
  will be persisted as a new property of the CS3 User Object. (Just a minor difference to Option 1,
  to avoid overloading the UserId with yet another property.)
* Option 3: Users are autoprovisioned on the 1st login. The value of the tenant id
  will be persisted in the user backend (usually LDAP in our case). There will not be any a new properties
  on CS3 UserID or User Object. Whenever a user lookup is done the tentant id of the
  requesting user (taken from the reva token) is used to construct the appropriate backend query
  (LDAP filter or subtree or ...) to only return users of the same tenant.
* Option 4: All of the above would also work without the autoprovisioning step.
  In that case an external LDAP server would need to be available that
  maintains the users' tenant assignments either via an explicit attribute on
  the users or by using a per-tenant subtrees.

## Decision Outcome

As part of the OIDC Connect Authentication OpenCloud will receive a tenant id via the
Access Token or Userinfo endpoint. When no shared LDAP server is available
OpenCloud will store the tenant id as part of the autoprovionsing process.
Either as via a per-tenant subtree (e.g. 'ou=tenantid,dc=example,dc=com') or as
an addtional user attribute. When a shared LDAP server is available the tenant
id must be available there either as a separate attribute of the user or as
part of the DN (i.e. separate per tenant subtrees).

More details TBD. Depending on which option we choose from the above list.

~~The CS3 API needs to be extended to support the tenant id. This will require adding a new
(optional if possible) field to the User Object (or the UserId inside the User Object). When requesting
users reva needs to assure that only user objects of the same tenant as the requesting user are
returned.~~

When creating a share there is a check in place that verifies that the sharer
and all sharees are part of the same tenant.


### Positive Consequences:

* All opencloud service are shared by all tenants, no per-tenant service required
* little changes to the internal APIs

### Negative consequences:

* requires changes to the CS3 API
* no clear data separation

