// Package runtime owns local deployment configuration, secrets and the process lock.
package runtime

import (
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type Config struct {
	Address, Database, Origin, KeyFile, JWTKeyFile, WebDir string
	JWTKey                                                 []byte
	Secure                                                 bool
	TrustedProxies                                         []string
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func Flags(fs *flag.FlagSet) *Config {
	c := &Config{}
	fs.StringVar(&c.Address, "addr", env("FRESHPLATE_ADDR", "127.0.0.1:8080"), "listen address")
	fs.StringVar(&c.Database, "db", env("FRESHPLATE_DB", "../data/freshplate.db"), "SQLite file")
	fs.StringVar(&c.Origin, "origin", env("FRESHPLATE_ORIGIN", "http://127.0.0.1:8080"), "exact browser origin")
	fs.StringVar(&c.KeyFile, "key", env("FRESHPLATE_KEY", "../data/csrf.key"), "CSRF key file")
	fs.StringVar(&c.JWTKeyFile, "jwt-key", env("FRESHPLATE_JWT_KEY", "../data/jwt.key"), "JWT signing key file")
	fs.StringVar(&c.WebDir, "web", env("FRESHPLATE_WEB", "../web/dist"), "built frontend directory")
	fs.BoolVar(&c.Secure, "secure", env("FRESHPLATE_SECURE", "false") == "true", "HTTPS cookies")
	fs.Func("trusted-proxies", "comma separated trusted proxy IPs/CIDRs", func(v string) error {
		if v != "" {
			c.TrustedProxies = strings.Split(v, ",")
		}
		return nil
	})
	return c
}

func (c Config) Validate() error {
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || c.Origin != u.Scheme+"://"+u.Host {
		return errors.New("origin must be an exact http(s) scheme, host and optional port")
	}
	if c.Secure != (u.Scheme == "https") {
		return errors.New("secure cookie mode must match origin scheme")
	}
	if _, _, err = net.SplitHostPort(c.Address); err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	paths := []string{c.Database, c.KeyFile, c.JWTKeyFile}
	for i, path := range paths {
		if path == "" {
			return errors.New("database, CSRF key and JWT key paths required")
		}
		for _, other := range paths[:i] {
			same, err := samePath(path, other)
			if err != nil {
				return err
			}
			if same {
				return errors.New("database, CSRF key and JWT key must have different paths")
			}
		}
	}
	for _, p := range c.TrustedProxies {
		if net.ParseIP(p) == nil {
			if _, _, e := net.ParseCIDR(p); e != nil {
				return errors.New("invalid trusted proxy")
			}
		}
	}
	if v := os.Getenv("FRESHPLATE_SECURE"); v != "" && v != "true" && v != "false" {
		return errors.New("FRESHPLATE_SECURE must be true or false")
	}
	return nil
}

// Resolve existing parents too, so aliases are rejected before key files exist.
func canonicalPath(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) || filepath.Dir(path) == path {
		return "", err
	}
	parent, err := canonicalPath(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func samePath(a, b string) (bool, error) {
	left, err := canonicalPath(a)
	if err != nil {
		return false, err
	}
	right, err := canonicalPath(b)
	if err != nil {
		return false, err
	}
	if left == right {
		return true, nil
	}
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	return aerr == nil && berr == nil && os.SameFile(ai, bi), nil
}

// Lock uses the database path so service and maintenance commands exclude each other.
func Lock(database string) (*os.File, error) {
	path, err := filepath.Abs(database)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	info, statErr := os.Lstat(path)
	if statErr == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("database must be a regular file, not a symbolic link")
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink > 1 {
			return nil, errors.New("database hard links are not supported")
		}
	} else if !os.IsNotExist(statErr) {
		return nil, statErr
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errors.New("database in use: stop the other service or maintenance command first")
	}
	return f, nil
}
func Unlock(f *os.File) error {
	if f == nil {
		return nil
	}
	err := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	closeErr := f.Close()
	return errors.Join(err, closeErr)
}

func Key(database, path string) ([]byte, error) {
	return secretKey(database, path, "CSRF")
}

func JWTKey(database, path string) ([]byte, error) {
	return secretKey(database, path, "JWT")
}

func secretKey(database, path, name string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return nil, fmt.Errorf("%s key must be a regular file with mode 0600", name)
		}
		key, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("invalid %s key length", name)
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if _, e := os.Stat(database); !os.IsNotExist(e) {
		return nil, fmt.Errorf("%s key missing for existing database; provision or restore the key before starting", name)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	_, writeErr := f.Write(key)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err = errors.Join(writeErr, syncErr, closeErr); err != nil {
		return nil, err
	}
	return key, nil
}
