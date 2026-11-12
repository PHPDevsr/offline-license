package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDBConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "database.json")
	dsnPath := filepath.Join(dir, "database.dsn")
	if err := os.WriteFile(configPath, []byte(`{
		"user": "issuer",
		"password": " $@:/?#%\"\\\n ",
		"address": "db.example.com:3306",
		"database": "licenses"
	}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dsnPath, []byte("issuer:existing@tcp(127.0.0.1:3306)/licenses?tls=false\r\n"), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := readDBConfig(configPath, dsnPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.Passwd != " $@:/?#%\"\\\n " {
		t.Fatal("JSON password was not preserved")
	}
	if config.User != "issuer" || config.Net != "tcp" || config.Addr != "db.example.com:3306" ||
		config.DBName != "licenses" || config.TLSConfig != "true" {
		t.Fatal("unexpected MySQL connection settings")
	}

	config, err = readDBConfig("", dsnPath)
	if err != nil {
		t.Fatal(err)
	}
	if config.Passwd != "existing" || config.DBName != "licenses" || config.TLSConfig != "false" {
		t.Fatal("existing DSN settings were not preserved")
	}
}

func TestDotEnvDatabaseConfig(t *testing.T) {
	t.Setenv("LICENSE_DB_PASSWORD", "")
	if err := os.Unsetenv("LICENSE_DB_PASSWORD"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LICENSE_DB_ADDRESS", "shell.example.com:3306")
	t.Setenv("LICENSE_DB_USER", "issuer")
	t.Setenv("LICENSE_DB_NAME", "licenses")
	t.Setenv("LICENSE_DB_TLS", "false")

	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(`LICENSE_DB_ADDRESS=file.example.com:3306
LICENSE_DB_PASSWORD=' $@:/?#% " \ \' '
`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	config, err := databaseConfig(os.Getenv)
	if err != nil {
		t.Fatal(err)
	}
	if config.Passwd != ` $@:/?#% " \ ' ` {
		t.Fatal("dotenv password was not preserved")
	}
	if config.Addr != "shell.example.com:3306" || config.User != "issuer" ||
		config.DBName != "licenses" || config.TLSConfig != "" {
		t.Fatal("unexpected MySQL connection settings")
	}
}
