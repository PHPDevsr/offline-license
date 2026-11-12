package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-sql-driver/mysql"
	"go.yaml.in/yaml/v4"
)

type serviceConfig struct {
	Server struct {
		Host        string `yaml:"host"`
		Port        int    `yaml:"port"`
		TLSCertFile string `yaml:"tls_cert_file"`
		TLSKeyFile  string `yaml:"tls_key_file"`
	} `yaml:"server"`
	Signing struct {
		KeyID          string `yaml:"key_id"`
		PrivateKeyFile string `yaml:"private_key_file"`
	} `yaml:"signing"`
	OperatorToken string         `yaml:"operator_token"`
	Database      databaseConfig `yaml:"database"`
}

type databaseConfig struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	Address  string `yaml:"address"`
	Name     string `yaml:"name"`
	TLS      bool   `yaml:"tls"`
}

func loadConfig(path string) (serviceConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return serviceConfig{}, fmt.Errorf("open configuration file: %w", err)
	}
	defer file.Close()

	var config serviceConfig
	config.Server.Port = 8443
	config.Database.TLS = true
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		// Decoder errors can include values such as passwords and tokens.
		return serviceConfig{}, errors.New("invalid YAML configuration")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return serviceConfig{}, errors.New("configuration must contain a single YAML document")
	}

	dir := filepath.Dir(path)
	for _, filePath := range []*string{&config.Signing.PrivateKeyFile, &config.Server.TLSCertFile, &config.Server.TLSKeyFile} {
		if *filePath != "" && !filepath.IsAbs(*filePath) {
			*filePath = filepath.Join(dir, *filePath)
		}
	}
	return config, nil
}

func (c databaseConfig) mysqlConfig() (*mysql.Config, error) {
	if c.User == "" || c.Address == "" || c.Name == "" {
		return nil, errors.New("set database.user, database.address, and database.name")
	}
	config := mysql.NewConfig()
	config.User = c.User
	config.Passwd = c.Password
	config.Net = "tcp"
	config.Addr = c.Address
	config.DBName = c.Name
	if c.TLS {
		config.TLSConfig = "true"
	}
	return config, nil
}
