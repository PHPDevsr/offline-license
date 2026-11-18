# Central License Service

This service uses Fiber v3 to issue `.license` files for registered products.
MySQL stores the product catalog and the licenses for each installation.
Applications verify licenses offline with an Ed25519 public key.
See the [offline license v1 specification](docs/license-v1.md).

## Database

Create a dedicated, empty MySQL database and apply
[`001_create_licenses.sql`](migrations/001_create_licenses.sql) and [`002_license_installations.sql`](migrations/002_license_installations.sql) in order using an
administrative account. Both `DATETIME(6)` columns require UTC timestamps.

Create a dedicated account for the issuer. This example assumes the
`license_service` database and `license_issuer` account already exist. Replace
the account host with the host for your deployment.

```sql
GRANT SELECT ON license_service.licenses TO 'license_issuer'@'issuer-host';
GRANT SELECT ON license_service.license_products TO 'license_issuer'@'issuer-host';
GRANT SELECT ON license_service.license_installations TO 'license_issuer'@'issuer-host';
```

Copy [`config.example.yaml`](config.example.yaml) to `config.yaml` and fill
in the read-only MySQL account settings. Single quotes preserve symbols in
the password. Write an apostrophe inside a single-quoted value as `''`.

```yaml
database:
  user: license_issuer
  password: 'example-$@:/?#%'
  address: db.example.com:3306
  name: license_service
  tls: true
```

TLS with certificate verification is enabled by default. Use a valid MySQL
certificate trusted by the service, or set `database.tls` to `false` for local
MySQL without TLS. Use a separate administrative account for migrations and
data changes.

## Deployment

Deploy on the License Server host. Create or mount an Ed25519 signing key
in PKCS#8 PEM format from the service secret store. To generate a key, run
`openssl genpkey -algorithm ED25519 -out signing.pem` with `umask 077`.
The Fiber listener uses HTTP/1.1. Leave both TLS file paths empty or omit
them to serve HTTP. To serve HTTPS directly, provide both files. The
listener then requires TLS 1.2 or later.

| Setting | Value |
| --- | --- |
| `signing.key_id` | ID of the public key distributed to applications |
| `signing.private_key_file` | Path to the private signing key |
| `operator_token` | Random operator token of at least 32 characters with no whitespace |
| `server.tls_cert_file` | Optional path to the HTTPS certificate chain in PEM format |
| `server.tls_key_file` | Optional path to the HTTPS private key in PEM format |
| `server.host` | Listener IP or hostname. An empty value listens on all interfaces |
| `server.port` | HTTP or HTTPS listener port. Defaults to `8443` |

The standalone binary reads `config.yaml` from its working directory.
Relative key and certificate paths are resolved from that file's directory.
Compose mounts `config.yaml` and `secrets/` as read-only. The files must
be readable by container UID/GID `65532:65532`.

For a standard rootful Docker installation on Linux, run these commands as
root from the deployment directory after creating the configuration and signing
key. The container group can read the files and traverse the secrets directory.

```sh
chown root:65532 config.yaml secrets secrets/signing.pem
chmod 0750 secrets
chmod 0640 config.yaml secrets/signing.pem
```

If Docker uses rootless mode or user namespace remapping, use the host GID
that maps to container GID `65532`.

Run `sh deploy.sh` to deploy with Compose. It reads `server.port` from
`config.yaml` and uses that value for both the container and published host
ports. Run it again after editing the configuration. The host bind address
defaults to `127.0.0.1` in `compose.yaml`.

To terminate TLS at a reverse proxy, configure the proxy to serve
`https://licenses.example.com` and forward requests to the service over
HTTP. Use the configured port in the proxy's upstream address. With the
default port, a proxy on the Docker host can use `http://127.0.0.1:8443`.
A proxy container on the same Docker network can use
`http://license-server:8443`. For a proxy on a separate host, publish the
port on a private address reachable by that proxy. Keep the HTTP listener
private so operators send their tokens through the HTTPS endpoint.
Restrict access to the operator network and use an address that matches the
certificate.

```sh
sh deploy.sh
docker compose run --rm --no-deps license-server print-keyring
```

Distribute the `print-keyring` output through trusted deployment
configuration. During rotation, distribute the new public key to all applications
using the keyring before activating the new signing key. Keep the previous
public key for as long as any active license uses it.

## Register products and installations

Register each product once. Every product uses the `phpdevsr-offline-license` issuer.
MySQL assigns `product_no` as the internal primary key. Set `product_id` to a
unique, stable code and configure the application verifier to expect that code.
The signed payload carries the code in its `product` field. You can edit
`product_name`, the display name, without affecting issued licenses.
Create one license per customer entitlement. Its product, expiration, and
grace deadline apply to all of its installations. Assign a distinct, stable
ID to every deployment and register each under the same license.

```sql
INSERT INTO license_products (product_id, product_name)
VALUES ('example-app', 'Example App');

INSERT INTO licenses (license_id, product_no, expired, expired_extra_gracia)
VALUES ('example-license-001',
        (SELECT product_no FROM license_products WHERE product_id = 'example-app'),
        '2027-12-31 00:00:00', '2028-01-07 00:00:00');

INSERT INTO license_installations (installation_id, license_no)
SELECT 'example-installation-001', license_no FROM licenses WHERE license_id = 'example-license-001';
```

Generate one key per license from 32 cryptographically random bytes encoded as
unpadded base64url. Store the SHA-256 digest of the encoded key as the 32 raw
bytes in `licenses.license_key_hash`. Apply the digest using a bound parameter
or pass its hex representation to `UNHEX` in the administrative update.
Distribute the encoded key to that license's installations through a secret
store and avoid putting it in shell history, SQL, or request logs. To rotate
it, provision a new digest and update those installations' configuration.

For an existing deployment, stop the License Server and pause administrative
writes before applying only migration 002. It copies the existing installation
mappings before removing their old column. Grant runtime SELECT access to
`license_installations` before starting the updated server. Existing licenses
can still be issued using their original IDs. Provision a key to enable renewal.

## Issue and renew licenses

Operators request license files over the HTTPS endpoint.
Set the shell variable `OPERATOR_TOKEN` to the `operator_token` value from
`config.yaml` before running the example. Customer applications use
the license file and public key for offline verification, and their license key
for renewal.

```sh
umask 077
curl --fail --silent --show-error \
  --request POST https://licenses.example.com/v1/licenses/issue \
  --header "Authorization: Bearer $OPERATOR_TOKEN" \
  --header 'Content-Type: application/json' \
  --data '{"license_id":"example-license-001","installation_id":"example-installation-001"}' \
  --output .license.new
```

| Status | Result |
| --- | --- |
| `200` | Signed `.license` file |
| `400` | Invalid request, such as invalid JSON or IDs, supplied dates, or a body larger than 4 KiB |
| `401` | Invalid operator token |
| `404` | License and installation ID pair not found |
| `422` | Invalid product configuration or dates, or the final expiration has passed |
| `503` | License database unavailable |

To renew, the administrator extends both dates on the existing license.
The application sends `POST /v1/licenses/renew` over HTTPS with JSON fields
`product_id`, `license_key`, and `installation_id`. A successful request
returns the signed `.license` as the response body with `Cache-Control: no-store`.
A malformed request returns 400. A well-formed request with an incorrect key,
product, or installation returns 404. A license without active dates returns
422. Database errors return 503. Legacy files without an envelope installation
ID must be reissued before deploying clients that require it.

## Releases

Tag the commit to release with `vMAJOR.MINOR.PATCH`, then push the tag.

```sh
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

After the CI matrix passes, GitHub Actions publishes release notes, binary
archives for Linux, Windows, and macOS (`amd64` and `arm64`), and
`SHA256SUMS`. Each archive contains the database schema and `config.example.yaml`.

## Checks

CI runs on branch pushes and pull requests on Linux, Windows, and macOS.
Each OS tests the minimum Go version from `go.mod` and the latest stable Go
release. Use a C compiler to run the race detector locally.

```sh
go install golang.org/x/tools/cmd/goimports@v0.52.0
gofmt -l .
goimports -l .
go mod verify
go mod tidy -diff
go vet ./...
go test -race ./...
CGO_ENABLED=0 go build -trimpath ./...
```
