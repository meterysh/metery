# Metery

Self-hosted usage-billing and entitlements backend.

## Services

### `metery.v1.CustomerService`

| RPC | Method | Path |
|---|---|---|
| `CreateCustomer` | `POST` | `/v1/customers` |
| `ListCustomers` | `GET` | `/v1/customers` |
| `GetCustomer` | `GET` | `/v1/customers/{id_or_key}` |
| `UpdateCustomer` | `PATCH` | `/v1/customers/{id_or_key}` |
| `DeactivateCustomer` | `DELETE` | `/v1/customers/{id_or_key}` |

### `metery.v1.MeterService`

| RPC | Method | Path |
|---|---|---|
| `CreateMeter` | `POST` | `/v1/meters` |
| `ListMeters` | `GET` | `/v1/meters` |
| `GetMeter` | `GET` | `/v1/meters/{id_or_slug}` |
| `ArchiveMeter` | `DELETE` | `/v1/meters/{id_or_slug}` |

### `metery.v1.FeatureService`

| RPC | Method | Path |
|---|---|---|
| `CreateFeature` | `POST` | `/v1/features` |
| `ListFeatures` | `GET` | `/v1/features` |
| `GetFeature` | `GET` | `/v1/features/{id_or_slug}` |
| `ArchiveFeature` | `DELETE` | `/v1/features/{id_or_slug}` |

### `metery.v1.EntitlementService`

| RPC | Method | Path |
|---|---|---|
| `CreateEntitlement` | `POST` | `/v1/customers/{customer_id_or_key}/entitlements` |
| `ListEntitlements` | `GET` | `/v1/customers/{customer_id_or_key}/entitlements` |
| `GetEntitlement` | `GET` | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}` |
| `DeleteEntitlement` | `DELETE` | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}` |
| `GetEntitlementValue` | `GET` | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}/value` |
| `ResetEntitlement` | `POST` | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}/reset` |

### `metery.v1.GrantService`

| RPC | Method | Path |
|---|---|---|
| `CreateGrant` | `POST` | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}/grants` |
| `ListGrants` | `GET` | `/v1/customers/{customer_id_or_key}/entitlements/{feature_id_or_slug}/grants` |
| `VoidGrant` | `DELETE` | `/v1/grants/{id}` |

### `metery.v1.EventService`

| RPC | Method | Path |
|---|---|---|
| `IngestEvent` | `POST` | `/v1/events` |

## Links

- [GitHub](https://github.com/meterysh/metery)