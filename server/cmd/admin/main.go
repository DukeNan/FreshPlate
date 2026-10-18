package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"freshplate/server/internal/logic"
	appRuntime "freshplate/server/internal/runtime"
	"freshplate/server/internal/store"
	"golang.org/x/term"
)

func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("password entry requires an interactive terminal")
	}
	fmt.Fprint(os.Stderr, "Password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	defer clear(first)
	fmt.Fprint(os.Stderr, "Confirm password: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	defer clear(second)
	if !bytes.Equal(first, second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: admin init|recover-password --username NAME [--db PATH --key PATH]")
	}
	command := os.Args[1]
	if command != "init" && command != "recover-password" {
		return errors.New("unknown admin command")
	}
	fs := flag.NewFlagSet("admin "+command, flag.ContinueOnError)
	cfg := appRuntime.Flags(fs)
	name := fs.String("username", "", "account username")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	normalized, err := logic.NormalizeUsername(*name)
	if err != nil {
		return err
	}
	lock, err := appRuntime.Lock(cfg.Database)
	if err != nil {
		return err
	}
	defer appRuntime.Unlock(lock)
	if _, err = appRuntime.Key(cfg.Database, cfg.KeyFile); err != nil {
		return err
	}
	st, err := store.Open(cfg.Database)
	if err != nil {
		return err
	}
	defer st.Close()
	if err = os.Chmod(cfg.Database, 0600); err != nil {
		return err
	}
	service, err := logic.New(st)
	if err != nil {
		return err
	}
	password, err := readPassword()
	if err != nil {
		return err
	}
	if command == "init" {
		_, err = service.InitAdmin(context.Background(), normalized, password)
	} else {
		err = service.RecoverAdmin(context.Background(), normalized, password)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Administrator operation completed; credentials were not printed.")
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}
