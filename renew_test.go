package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestRenewRequest(t *testing.T) {
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	body := fmt.Sprintf(`{"product_id":"test-app","license_key":"%s","installation_id":"install-a"}`, key)
	req, digest, err := readRenewRequest([]byte(body))
	if err != nil || req.ProductID != "test-app" || req.InstallationID != "install-a" || digest != sha256.Sum256([]byte(key)) {
		t.Fatal("valid renewal request was rejected")
	}
	for _, invalid := range []string{
		`{}`,
		strings.Replace(body, key, key+"=", 1),
		strings.Replace(body, "install-a", "../install-a", 1),
		strings.TrimSuffix(body, "}") + `,"expired":"2099-01-01"}`,
		body + `{}`,
	} {
		if _, _, err := readRenewRequest([]byte(invalid)); err == nil {
			t.Fatal("accepted invalid renewal request")
		}
	}
}

func TestRenewHTTPErrorResponses(t *testing.T) {
	db, err := openDB(mysql.NewConfig())
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	app := (issuer{db: db}).routes()
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	for body, status := range map[string]int{
		`{}`: http.StatusBadRequest,
		fmt.Sprintf(`{"product_id":"test-app","license_key":"%s","installation_id":"install-a"}`, key): http.StatusServiceUnavailable,
	} {
		resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/v1/licenses/renew", strings.NewReader(body)))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != status || resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("got HTTP %d, want %d with no-store", resp.StatusCode, status)
		}
	}
}

func TestRenewMySQL(t *testing.T) {
	// Use a dedicated empty database. This test applies both real migrations.
	dsn := os.Getenv("LICENSE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set LICENSE_TEST_MYSQL_DSN to a dedicated empty test database")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || cfg.DBName == "" {
		t.Fatal("configure a DSN with a test database")
	}
	cfg.MultiStatements = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var tables int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE()").Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("test database must be empty: %d tables, %v", tables, err)
	}
	defer func() {
		_, err := db.Exec("DROP TABLE IF EXISTS license_installations, licenses, license_products")
		if err != nil {
			t.Error(err)
		}
	}()
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.Exec(statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	migrate := func(path string) {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		exec(string(data))
	}
	migrate("migrations/001_create_licenses.sql")
	exec(`INSERT INTO license_products (product_no, product_id, product_name) VALUES (1, 'test-app', 'Test')`)
	exec(`INSERT INTO licenses (license_no, product_no, license_id, installation_id, expired, expired_extra_gracia)
		VALUES (1, 1, 'license-a', 'install-a', '2099-01-01', '2099-01-02'),
		       (2, 1, 'license-b', 'install-c', '2099-01-01', '2099-01-02')`)
	migrate("migrations/002_license_installations.sql")
	exec(`INSERT INTO license_installations (installation_id, license_no) VALUES ('install-b', 1)`)

	pub, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	const operator = "test-operator"
	app := (issuer{db: db, keyID: "test", key: private, tokenHash: sha256.Sum256([]byte(operator))}).routes()
	post := func(path, body string, status int, installationID string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/licenses/"+path, strings.NewReader(body))
		if path == "issue" {
			req.Header.Set("Authorization", "Bearer "+operator)
		}
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			t.Fatalf("%s returned %d, want %d", path, resp.StatusCode, status)
		}
		if status != http.StatusOK {
			return
		}
		data, err := io.ReadAll(resp.Body)
		var signed envelope
		if err != nil || json.Unmarshal(data, &signed) != nil || signed.InstallationID != installationID ||
			resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("invalid license response")
		}
		payload, payloadErr := base64.RawURLEncoding.DecodeString(signed.Payload)
		signature, sigErr := base64.RawURLEncoding.DecodeString(signed.Signature)
		var issued claims
		if payloadErr != nil || sigErr != nil || !ed25519.Verify(pub, payload, signature) ||
			json.Unmarshal(payload, &issued) != nil || issued.LicenseID != "license-a" ||
			issued.Product != "test-app" || issued.InstallationID != installationID || issued.Expired.Year() != 2099 {
			t.Fatal("license did not preserve the signed identity and expiry")
		}
	}
	post("issue", `{"license_id":"license-a","installation_id":"install-a"}`, http.StatusOK, "install-a")
	key := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	digest := sha256.Sum256([]byte(key))
	exec("UPDATE licenses SET license_key_hash = ? WHERE license_no = 1", digest[:])
	renew := func(product, key, installation string, status int) {
		t.Helper()
		post("renew", fmt.Sprintf(`{"product_id":"%s","license_key":"%s","installation_id":"%s"}`, product, key, installation), status, installation)
	}
	renew("test-app", key, "install-a", http.StatusOK)
	renew("test-app", key, "install-b", http.StatusOK)
	renew("test-app", key, "install-c", http.StatusNotFound)
	renew("wrong-app", key, "install-a", http.StatusNotFound)
	wrongKey := base64.RawURLEncoding.EncodeToString(append([]byte{1}, make([]byte, 31)...))
	renew("test-app", wrongKey, "install-a", http.StatusNotFound)
	exec("UPDATE licenses SET expired = '2001-01-01' WHERE license_no = 1")
	renew("test-app", key, "install-a", http.StatusUnprocessableEntity)
}
