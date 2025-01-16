# IDP

This service provides a builtin minimal OpenID Connect provider based on [LibreGraph Connect (lico)](https://github.com/libregraph/lico) for OpenCloud.

It is mainly targeted at smaller installations. For larger setups it is recommended to replace IDP with an external OpenID Connect Provider.

By default, it is configured to use the OpenCloud IDM service as its LDAP backend for looking up and authenticating users. Other backends like an external LDAP server can be configured via a set of [enviroment variables](https://docs.opencloud.eu/services/idp/configuration/#environment-variables).

Note that translations provided by the IDP service are not maintained via OpenCloud but part of the embedded  [LibreGraph Connect Identifier](https://github.com/libregraph/lico/tree/master/identifier) package.
