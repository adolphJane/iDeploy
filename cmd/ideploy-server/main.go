package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	mysql "github.com/go-sql-driver/mysql"
	"ideploy/internal/sharedserver"
)

func main() {
	tokenFile := os.Getenv("IDEPLOY_SERVER_TOKEN_FILE")
	if tokenFile == "" {
		log.Fatal("IDEPLOY_SERVER_TOKEN_FILE is required")
	}
	driver := os.Getenv("IDEPLOY_DATABASE_DRIVER")
	if driver == "" {
		driver = "postgres"
	}
	dsn, err := databaseDSN(driver, os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server, err := sharedserver.OpenWithDriver(ctx, driver, dsn, strings.TrimSpace(string(token)))
	if err != nil {
		log.Fatal(err)
	}
	defer server.Close()
	address := os.Getenv("IDEPLOY_SERVER_LISTEN")
	if address == "" {
		address = "127.0.0.1:8765"
	}
	httpServer := &http.Server{Addr: address, Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()
	log.Printf("iDeploy shared server listening on %s", address)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func databaseDSN(driver string, getenv func(string) string) (string, error) {
	if driver == "mysql" && getenv("IDEPLOY_DB_HOST") != "" {
		host, user, password, name := getenv("IDEPLOY_DB_HOST"), getenv("IDEPLOY_DB_USER"), getenv("IDEPLOY_DB_PASSWORD"), getenv("IDEPLOY_DB_NAME")
		if passwordFile := getenv("IDEPLOY_DB_PASSWORD_FILE"); passwordFile != "" {
			data, err := os.ReadFile(passwordFile)
			if err != nil {
				return "", errors.New("cannot read MySQL password file")
			}
			password = strings.TrimSuffix(string(data), "\n")
		}
		if user == "" || password == "" || name == "" {
			return "", errors.New("MySQL host, user, password and database name are required")
		}
		port := getenv("IDEPLOY_DB_PORT")
		if port == "" {
			port = "3306"
		}
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return "", errors.New("MySQL port must be between 1 and 65535")
		}
		config := mysql.NewConfig()
		config.Net, config.Addr, config.User, config.Passwd, config.DBName = "tcp", net.JoinHostPort(host, port), user, password, name
		tlsMode := getenv("IDEPLOY_DB_TLS_MODE")
		switch tlsMode {
		case "", "verify":
			config.TLSConfig = "true"
		case "skip-verify":
			config.TLSConfig = "skip-verify"
		default:
			return "", errors.New("IDEPLOY_DB_TLS_MODE must be verify or skip-verify")
		}
		config.ParseTime = true
		return config.FormatDSN(), nil
	}
	if value := getenv("IDEPLOY_DATABASE_URL"); value != "" {
		return value, nil
	}
	path := getenv("IDEPLOY_DATABASE_URL_FILE")
	if path == "" {
		return "", errors.New("database environment variables or IDEPLOY_DATABASE_URL_FILE are required")
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(value)), nil
}
