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
	"net"
	"os"
	"strconv"
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
	command := ""
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || (os.Args[1] != "print-keyring" && os.Args[1] != "print-port") {
			return errors.New("expected no argument, print-keyring, or print-port")
		}
		command = os.Args[1]
	}
	config, err := loadConfig("config.yaml")
	if err != nil {
		return err
	}
	if command != "print-keyring" && (config.Server.Port < 1 || config.Server.Port > 65535) {
		return errors.New("server.port must be between 1 and 65535")
	}
	if command == "print-port" {
		_, err := fmt.Println(config.Server.Port)
		return err
	}
	key, err := readPrivateKey(config.Signing.PrivateKeyFile)
	if err != nil {
		return err
	}
	kid := config.Signing.KeyID
	if !validID(kid) {
		return errors.New("invalid signing.key_id")
	}
	if command == "print-keyring" {
		publicKey := base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey))
		keyring, err := json.Marshal(map[string]string{kid: publicKey})
		if err != nil {
			return err
		}
		_, err = fmt.Println(base64.StdEncoding.EncodeToString(keyring))
		return err
	}

	token := config.OperatorToken
	if len(token) < 32 || strings.ContainsAny(token, " \t\r\n") {
		return errors.New("operator_token must have at least 32 characters and no whitespace")
	}
	database, err := config.Database.mysqlConfig()
	if err != nil {
		return err
	}
	db, err := openDB(database)
	if err != nil {
		return err
	}
	defer db.Close()

	addr := net.JoinHostPort(config.Server.Host, strconv.Itoa(config.Server.Port))
	listen := fiber.ListenConfig{ListenerNetwork: "tcp"}
	certFile, keyFile := config.Server.TLSCertFile, config.Server.TLSKeyFile
	if (certFile == "") != (keyFile == "") {
		return errors.New("configure both server.tls_cert_file and server.tls_key_file for HTTPS")
	}
	if certFile != "" {
		certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return fmt.Errorf("load HTTPS certificate: %w", err)
		}
		listen.TLSConfig = &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{certificate},
		}
	}
	service := issuer{db: db, keyID: kid, key: key, tokenHash: sha256.Sum256([]byte(token))}
	return service.routes().Listen(addr, listen)
}

func openDB(config *mysql.Config) (*sql.DB, error) {
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
