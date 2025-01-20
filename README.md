![OpenCloud logo](opencloud_logo.png)

# Server Backend


> [!TIP]
> For general information about OpenCloud and how to install please visit [OpenCloud on Github](https://github.com/opencloud-eu/) and [OpenCloud GmbH](https://opencloud.eu).

This is one of the main repositories of OpenCloud. It contains most of the code for the backend that is run on a server and is mostly written in golang.

## Technology

Important information for contributors about the technology in use. 

### Authentication

The OpenCloud backend authenticates users via [OpenID Connect](https://openid.net/connect/) using either an external IdP like [Keycloak](https://www.keycloak.org/) or the embedded [LibreGraph Connect](https://github.com/libregraph/lico) identity provider.

### Database

The OpenCloud backend does not use a database. It stores all data in the filesystem. By default, the root directory of the backend is `$HOME/.opencloud/`.

## Getting Involved

The OpenCloud server is released under [Apache 2.0](LICENSE). The project is very happy to receive contributions in all forms. Start hacking now 😃

### Build OpenCloud

To build the backend, follow the following instructions:

``` console
cd opencloud
make build
```
That will produce the binary `bin/opencloud`.

For more information consult the [Development Documentation](https://docs.opencloud.eu/opencloud/).

Please always refer to our [Contribution Guidelines](https://github.com/opencloud-eu/opencloud/blob/master/CONTRIBUTING.md).

## Security

See the [Security Aspects](https://docs.opencloud.eu/security/) for security related topics.

If you find a security issue, please contact [security@opencloud.eu](mailto:security@opencloud.eu) first.
