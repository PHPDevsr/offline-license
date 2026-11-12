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
	"strconv"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/dotenv"
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
	if err := loadDotEnv(".env"); err != nil {
		return err
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
	config, err := databaseConfig(os.Getenv)
	if err != nil {
		return err
	}
	db, err := openDB(config)
	if err != nil {
		return err
	}
	defer db.Close()

	addr := os.Getenv("LICENSE_LISTEN_ADDR")
	if addr == "" {
		port := os.Getenv("LICENSE_PORT")
		if port == "" {
			port = "8443"
		}
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return errors.New("LICENSE_PORT must be a port number between 1 and 65535")
		}
		addr = ":" + port
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

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open environment file: %w", err)
	}
	defer file.Close()

	values, err := dotenv.ParseWithLookup(file, os.LookupEnv)
	if err != nil {
		return errors.New("invalid .env configuration")
	}
	for name, value := range values {
		if _, exists := os.LookupEnv(name); exists {
			continue
		}
		if err := os.Setenv(name, value); err != nil {
			return errors.New("could not load .env configuration")
		}
	}
	return nil
}

func databaseConfig(getenv func(string) string) (*mysql.Config, error) {
	user := getenv("LICENSE_DB_USER")
	password := getenv("LICENSE_DB_PASSWORD")
	address := getenv("LICENSE_DB_ADDRESS")
	database := getenv("LICENSE_DB_NAME")
	tlsValue := getenv("LICENSE_DB_TLS")
	if user == "" && password == "" && address == "" && database == "" && tlsValue == "" {
		return readDBConfig(getenv("LICENSE_DB_CONFIG_FILE"), getenv("LICENSE_DB_DSN_FILE"))
	}
	useTLS := true
	if tlsValue != "" {
		var err error
		useTLS, err = strconv.ParseBool(tlsValue)
		if err != nil {
			return nil, errors.New("LICENSE_DB_TLS must be a boolean")
		}
	}
	config, err := mysqlConfig(user, password, address, database, useTLS)
	if err != nil {
		return nil, errors.New("set LICENSE_DB_USER, LICENSE_DB_ADDRESS, and LICENSE_DB_NAME")
	}
	return config, nil
}

func readDBConfig(configPath, dsnPath string) (*mysql.Config, error) {
	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("read LICENSE_DB_CONFIG_FILE: %w", err)
		}
		var settings struct {
			User     string `json:"user"`
			Password string `json:"password"`
			Address  string `json:"address"`
			Database string `json:"database"`
			TLS      *bool  `json:"tls"`
		}
		if err := json.Unmarshal(data, &settings, json.RejectUnknownMembers(true)); err != nil {
			return nil, errors.New("invalid JSON in LICENSE_DB_CONFIG_FILE")
		}
		config, err := mysqlConfig(settings.User, settings.Password, settings.Address, settings.Database, settings.TLS == nil || *settings.TLS)
		if err != nil {
			return nil, errors.New("LICENSE_DB_CONFIG_FILE requires user, address, and database")
		}
		return config, nil
	}

	if dsnPath == "" {
		return nil, errors.New("set LICENSE_DB_USER, LICENSE_DB_ADDRESS, and LICENSE_DB_NAME")
	}
	data, err := os.ReadFile(dsnPath)
	if err != nil {
		return nil, fmt.Errorf("read LICENSE_DB_DSN_FILE: %w", err)
	}
	dsn := strings.TrimRight(string(data), "\r\n")
	if dsn == "" {
		return nil, errors.New("empty license database DSN")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, errors.New("invalid license database DSN")
	}
	return config, nil
}

func mysqlConfig(user, password, address, database string, useTLS bool) (*mysql.Config, error) {
	if user == "" || address == "" || database == "" {
		return nil, errors.New("missing required MySQL settings")
	}
	config := mysql.NewConfig()
	config.User = user
	config.Passwd = password
	config.Net = "tcp"
	config.Addr = address
	config.DBName = database
	if useTLS {
		config.TLSConfig = "true"
	}
	return config, nil
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
