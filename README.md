# Central License Service

This service uses Fiber v3 to issue `.license` files for registered products.
MySQL stores the product catalog and the licenses for each installation.
Applications verify licenses offline with an Ed25519 public key.
See the [offline license v1 specification](docs/license-v1.md).

## Database

Create a dedicated, empty MySQL database and apply
[`001_create_licenses.sql`](migrations/001_create_licenses.sql) once using an
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

## Register, issue, and renew a license

Use a database administrator account to provision licenses after applying
`migrations/001_create_licenses.sql`. The License Server's runtime account
only needs the SELECT grants described above.

### 1. Register the product and generate a license key

Register the application's product code once. For WOM, the expected
`product_id` is `eclaim-wom-api`. Skip the insert if the product already exists.

```sql
INSERT INTO license_products (product_id, product_name)
VALUES ('eclaim-wom-api', 'ETIQA CLAIM');
```

Generate one key per customer license. The raw `LICENSE_KEY` is a renewal
credential shared by all installations under that license. The database stores
the SHA-256 digest of the **encoded key text**.

```bash
set +x
LICENSE_KEY=$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=\n')
LICENSE_KEY_HASH=$(printf '%s' "$LICENSE_KEY" | openssl dgst -sha256 -r | cut -d' ' -f1)
printf 'SHA-256 hash for SQL: %s\n' "$LICENSE_KEY_HASH"
```

Save the raw `LICENSE_KEY` from the shell in your secret store. Use the
displayed 64-character hex digest in the SQL below. Keep the raw key out of
SQL statements and request logs.

### 2. Create the license and register its installations

Choose one stable `license_id` for the entitlement and a distinct
`installation_id` for each deployment. The product, key, and dates apply to
every installation under that license. Use UTC for both dates. `expired`
starts the grace period and `expired_extra_gracia` ends it.

Run this as one transaction and commit after both inserts succeed. Roll back
the transaction if either insert fails.

```sql
START TRANSACTION;

INSERT INTO licenses (
    product_no, license_id, license_key_hash, expired, expired_extra_gracia
) VALUES (
    (SELECT product_no FROM license_products WHERE product_id = 'eclaim-wom-api'),
    'wom-client-a-001',
    UNHEX('<64-character hex digest>'),
    '2027-12-31 00:00:00',
    '2028-01-07 00:00:00'
);

SET @license_no = LAST_INSERT_ID();

INSERT INTO license_installations (installation_id, license_no)
VALUES ('wom-client-a-prod', @license_no),
       ('wom-client-a-dr', @license_no);

COMMIT;
```

### 3. Issue and install the initial files

Load `OPERATOR_TOKEN` from the same secret used for `operator_token` in the
server configuration. Send one HTTPS issue request per installation.

```bash
umask 077
for installation_id in wom-client-a-prod wom-client-a-dr; do
  printf 'Authorization: Bearer %s\n' "$OPERATOR_TOKEN" |
    curl --fail --silent --show-error \
      --request POST https://licenses.example.com/v1/licenses/issue \
      --header @- \
      --header 'Content-Type: application/json' \
      --data "$(printf '{"license_id":"wom-client-a-001","installation_id":"%s"}' "$installation_id")" \
      --output "${installation_id}.license" || break
done
```

HTTP 200 returns a signed file. Install the matching file on each deployment
as `license/.license` and configure the trusted public keyring. Reissue legacy
files that lack an envelope `installation_id` before deploying WOM versions
that require it.

For WOM, set these values in each deployment's `.env`. Both installations
use the same raw key and each keeps its own signed file.

```dotenv
LICENSE_KEY=<raw key from the secret store>
LICENSE_RENEW_URL=https://licenses.example.com/v1/licenses/renew
```

### 4. Extend the dates and renew

Extend both dates on the existing license. The example assumes the dates from
step 2 are still registered. Check that the new `expired` is in the future and
later than the previous value. Keep `expired_extra_gracia` at or after
`expired` and at or after its previous value.

```sql
UPDATE licenses
SET expired = '2028-12-31 00:00:00',
    expired_extra_gracia = '2029-01-07 00:00:00'
WHERE license_id = 'wom-client-a-001';
```

The WOM API requests renewal at its current file's `expired` time and retries
during grace. The License Server looks up the product and installation
associated with the submitted key and signs a new file using the stored dates.
WOM verifies the signature, identity, and dates before replacing its local file.
Its workers read the updated file from the shared directory.

To check renewal manually after extending the dates, load the raw
`LICENSE_KEY` from the secret store. Send the request over HTTPS and save the
result to a separate file for verification.

```bash
set +x
umask 077
printf '{"product_id":"eclaim-wom-api","license_key":"%s","installation_id":"wom-client-a-prod"}' "$LICENSE_KEY" |
  curl --fail --silent --show-error \
    --request POST https://licenses.example.com/v1/licenses/renew \
    --header 'Content-Type: application/json' \
    --data-binary @- \
    --output wom-client-a-prod.renewed.license
```

| Status | Issue | Renew |
| --- | --- | --- |
| `200` | Signed `.license` file | Signed `.license` file with the registered dates |
| `400` | Invalid request | Invalid request, including a malformed key |
| `401` | Invalid operator token | — |
| `404` | License and installation pair not found | Key, product, and installation do not match a registered license |
| `422` | Invalid license configuration or final expiration has passed | Invalid license configuration or `expired` is due or past |
| `503` | License database unavailable | License database unavailable |

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
