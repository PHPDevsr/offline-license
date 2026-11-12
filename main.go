package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json/v2"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/gofiber/fiber/v3"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) > 1 && (len(os.Args) != 2 || os.Args[1] != "print-keyring") {
		return errors.New("expected no argument or print-keyring")
	}
	key, err := readPrivateKey(os.Getenv("LICENSE_PRIVATE_KEY_FILE"))
	if err != nil {
		return err
	}
	kid := os.Getenv("LICENSE_KEY_ID")
	if !validID(kid) {
		return errors.New("invalid LICENSE_KEY_ID")
	}
	if len(os.Args) == 2 {
		publicKey := base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
		keyring, err := json.Marshal(map[string]string{kid: publicKey})
		if err != nil {
			return err
		}
		_, err = fmt.Println(base64.StdEncoding.EncodeToString(keyring))
		return err
	}

	token := os.Getenv("LICENSE_OPERATOR_TOKEN")
	if len(token) < 32 || strings.ContainsAny(token, " \t\r\n") {
		return errors.New("require LICENSE_OPERATOR_TOKEN (at least 32 characters, no whitespace)")
	}
	dsn, err := os.ReadFile(os.Getenv("LICENSE_DB_DSN_FILE"))
	if err != nil {
		return fmt.Errorf("read LICENSE_DB_DSN_FILE: %w", err)
	}
	db, err := openDB(strings.TrimRight(string(dsn), "\r\n"))
	if err != nil {
		return err
	}
	defer db.Close()

	addr := os.Getenv("LICENSE_LISTEN_ADDR")
	if addr == "" {
		addr = ":8443"
	}
	certificate, err := tls.LoadX509KeyPair(os.Getenv("LICENSE_TLS_CERT_FILE"), os.Getenv("LICENSE_TLS_KEY_FILE"))
	if err != nil {
		return fmt.Errorf("load HTTPS certificate: %w", err)
	}
	service := issuer{db: db, keyID: kid, key: key, tokenHash: sha256.Sum256([]byte(token))}
	return service.routes().Listen(addr, fiber.ListenConfig{
		ListenerNetwork: "tcp",
		TLSConfig: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		},
	})
}

func openDB(dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, errors.New("empty license database DSN")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, errors.New("invalid license database DSN")
	}
	connector, err := mysql.NewConnector(config)
	if err != nil {
		return nil, errors.New("invalid license database connection settings")
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(3 * time.Minute)
	return db, nil
}

func readPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(data)
	if block == nil || block.Type != "PRIVATE KEY" || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("expected a PKCS#8 PEM private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("expected an Ed25519 private key")
	}
	return key, nil
}
