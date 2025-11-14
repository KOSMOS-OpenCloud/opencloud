# Storage PublicLink

The `storage-publiclink` service provides storage backend functionality for public link shares in OpenCloud. It implements the CS3 storage provider interface specifically for managing public link shared resources.

## Overview

This service is part of the storage services family and is responsible for:
- Storing metadata about public link shares
- Providing access to publicly shared resources
- Managing public link tokens and permissions
- Handling anonymous access to shared content

## Integration

The storage-publiclink service integrates with:
- `sharing` service - Creates and manages public link shares
- `gateway` service - Routes requests to publicly shared resources
- `frontend` and `ocdav` - Provide HTTP/WebDAV access to public links
- Storage drivers - Accesses the actual file content

## Storage Registry

The service is registered in the gateway's storage registry with:
- Provider ID: `7993447f-687f-490d-875c-ac95e89a62a4`
- Mount point: `/public`
- Space types: `grant` and `mountpoint`

See the `gateway` README for more details on storage registry configuration.

## Access Control

Public link shares can be configured with:
- Password protection
- Expiration dates
- Read-only or read-write permissions
- Download limits

## Scalability

The storage-publiclink service can be scaled horizontally. When running multiple instances, ensure that the storage backend configuration is identical across all instances.
