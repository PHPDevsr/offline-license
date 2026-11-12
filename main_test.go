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
