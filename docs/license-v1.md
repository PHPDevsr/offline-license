# Offline License v1 Specification

## Identity

- `product` is the stable application code stored as `product_id` in the License Server catalog.
- Every product uses the issuer `phpdevsr-offline-license`.
- `license_id` and `installation_id` are each unique across the License Server.
  Each license belongs to one product. Assign a distinct, stable installation
  ID to each product deployment.
- IDs, product codes, issuers, and `kid` are case-sensitive ASCII strings
  of 1–128 characters drawn from `A-Z a-z 0-9 _ -`.

## Issuance

The operator sends `POST /v1/licenses/issue` over HTTPS with
`Authorization: Bearer <token>` and a JSON object containing exactly two
strings, `license_id` and `installation_id`. The License Server reads the
product and dates from the database. An HTTP `200` response contains
the `.license` file.

To renew a license, the administrator updates the dates and the operator
sends the same ID pair again. The previous file remains valid until its own
final expiration.

## File format

New files include `installation_id` alongside `kid`, `payload`, and
`signature` in the JSON envelope.

```json
{
  "kid": "signing-2026",
  "installation_id": "example-installation-001",
  "payload": "<base64url payload>",
  "signature": "<base64url signature>"
}
```

The verifier uses `kid` to select a trusted Ed25519 public key. Both
`payload` and `signature` use unpadded RFC 4648 base64url. The issuer signs
the UTF-8 JSON payload bytes before encoding them. Previously issued files
may omit the envelope `installation_id`. When present, it must be a string
that exactly matches the ID in the verified payload.

| Payload field | Type and value |
| --- | --- |
| `version` | Integer `1` |
| `issuer` | String `phpdevsr-offline-license` |
| `product` | Stable product code string (`product_id` in the product catalog) |
| `license_id` | License ID string |
| `installation_id` | Installation ID string |
| `expired` | Start of the grace period as an RFC 3339 UTC string ending in `Z` |
| `expired_extra_gracia` | Final expiration as an RFC 3339 UTC string ending in `Z` |

Timestamps can include up to six fractional digits. Both dates must be valid,
and the final expiration must be at or after the start of the grace period.

## Application verification

1. Provision the keyring through trusted deployment configuration. Configure
   the application with issuer `phpdevsr-offline-license`, its product code, and the
   local installation ID. The `print-keyring` command outputs a standard
   base64-encoded JSON object that maps each `kid` to a standard
   base64-encoded 32-byte Ed25519 public key.
2. Limit the license file to 16 KiB. Parse the envelope, look up the public
   key by `kid`, and verify the signature using the decoded payload bytes
   directly.
3. After verifying the signature, validate the version, issuer, product,
   installation ID, and dates. Compare the signed installation ID with the
   optional envelope ID and the local installation ID. Reject licenses for
   another product even if both products trust the same signing key.
4. Check the current UTC time locally at startup and while accepting work.
   The license is active before `expired`. From `expired` until
   `expired_extra_gracia`, the application may continue accepting work during
   the grace period. Stop accepting new work at final expiration.

Integrate this verifier once per application. Product registration,
installation provisioning, and renewals use the License Server database.

Deploy verifiers that accept the envelope `installation_id` to all
application processes before upgrading the License Server. Verify each
process accepts both old and new files before issuing the new format.
