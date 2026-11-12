package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`server:
  tls_cert_file: ./secrets/server.crt
signing:
  private_key_file: ./secrets/signing.pem
database:
  user: issuer
  password: ' $@:/?#% " \ '' '
  address: db.example.com:3306
  name: licenses
`), 0600); err != nil {
		t.Fatal(err)
	}
	config, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	database, err := config.Database.mysqlConfig()
	if err != nil {
		t.Fatal(err)
	}
	if database.Passwd != ` $@:/?#% " \ ' ` || database.TLSConfig != "true" {
		t.Fatal("expected the literal password and default MySQL TLS")
	}
	if config.Server.Port != 8443 ||
		config.Server.TLSCertFile != filepath.Join(dir, "secrets", "server.crt") ||
		config.Signing.PrivateKeyFile != filepath.Join(dir, "secrets", "signing.pem") {
		t.Fatal("unexpected default port or relative file paths")
	}
}

func TestLoadConfigRejectsInvalidDocuments(t *testing.T) {
	for _, document := range []string{
		"server:\n  porrt: 8443\n",
		"server:\n  port: 8443\n  port: 19443\n",
		"server: {}\n---\ndatabase: {}\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(document), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(path); err == nil {
			t.Fatalf("accepted invalid YAML configuration %q", document)
		}
	}
}
