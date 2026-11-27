package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"
)

const (
	maxCredValue = 512
	maxCredFile  = 16 << 10
)

func loadCredentials() (string, string, error) {
	id := os.Getenv("PCO_APP_ID")
	secret := os.Getenv("PCO_SECRET")
	switch {
	case id != "" && secret != "":
		return id, secret, nil
	case id != "" || secret != "":
		return "", "", errors.New("set both PCO_APP_ID and PCO_SECRET")
	}

	path, err := credPath()
	if err != nil {
		return "", "", err
	}
	id, secret, ok, err := readCredFile(path)
	if err != nil {
		return "", "", err
	}
	if ok {
		return id, secret, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", "", errors.New("set PCO_APP_ID and PCO_SECRET to a Planning Center personal access token (https://api.planningcenteronline.com/oauth/applications)")
	}

	fmt.Fprintln(os.Stderr, "Planning Center personal access token.")
	fmt.Fprintln(os.Stderr, "Create one at https://api.planningcenteronline.com/oauth/applications")
	fmt.Fprintln(os.Stderr)
	fmt.Fprint(os.Stderr, "Application ID: ")
	id, err = readLine(os.Stdin)
	if err != nil {
		return "", "", err
	}
	id = strings.TrimSpace(id)
	fmt.Fprint(os.Stderr, "Secret: ")
	secretBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", "", err
	}
	secret = strings.TrimSpace(string(secretBytes))
	if err := validCred(id, secret); err != nil {
		return "", "", err
	}

	fmt.Fprintf(os.Stderr, "Save these in %s for next time? [y/N] ", path)
	answer, err := readLine(os.Stdin)
	if err != nil {
		return "", "", err
	}
	if yes(answer) {
		if err := saveCreds(path, id, secret); err != nil {
			return "", "", err
		}
		fmt.Fprintf(os.Stderr, "Saved %s\n", path)
	}
	return id, secret, nil
}

func credPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pco-downloader", "credentials"), nil
}

func readCredFile(path string) (string, string, bool, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", "", false, fmt.Errorf("%s is a symlink", path)
	}
	if !fi.Mode().IsRegular() {
		return "", "", false, fmt.Errorf("%s is not a regular file", path)
	}
	if fi.Size() > maxCredFile {
		return "", "", false, fmt.Errorf("%s is too large", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", "", false, fmt.Errorf("%s is readable by others: %w", path, err)
		}
	}
	f, err := os.Open(path) //nolint:gosec // G304: path is the credential file after the symlink check
	if err != nil {
		return "", "", false, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxCredFile+1))
	if err != nil {
		return "", "", false, err
	}
	if int64(len(data)) > maxCredFile {
		return "", "", false, fmt.Errorf("%s is too large", path)
	}
	id, secret, err := parseCredFile(string(data))
	if err != nil {
		return "", "", false, fmt.Errorf("%s: %w", path, err)
	}
	if id == "" && secret == "" {
		return "", "", false, nil
	}
	if err := validCred(id, secret); err != nil {
		return "", "", false, fmt.Errorf("%s: %w", path, err)
	}
	return id, secret, true, nil
}

func parseCredFile(data string) (string, string, error) {
	var id, secret string
	for n, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return "", "", fmt.Errorf("line %d is not KEY=VALUE", n+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			unquoted, err := strconv.Unquote(value)
			if err != nil {
				return "", "", err
			}
			value = unquoted
		}
		switch key {
		case "PCO_APP_ID":
			id = value
		case "PCO_SECRET":
			secret = value
		default:
			return "", "", fmt.Errorf("line %d has an unknown credential key", n+1)
		}
	}
	return id, secret, nil
}

func validCred(id, secret string) error {
	if id == "" || secret == "" {
		return errors.New("application id and secret are both required")
	}
	if len(id) > maxCredValue || len(secret) > maxCredValue {
		return errors.New("application id or secret is too long")
	}
	if strings.ContainsAny(id, "\r\n\x00") || strings.ContainsAny(secret, "\r\n\x00") {
		return errors.New("application id and secret cannot contain newlines")
	}
	return nil
}

func saveCreds(path, id, secret string) error {
	if err := validCred(id, secret); err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	if err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	di, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if di.Mode()&os.ModeSymlink != 0 || !di.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // G302: 0700 is the directory mode, not a file mode
		return err
	}
	content := []byte("PCO_APP_ID=" + id + "\nPCO_SECRET=" + secret + "\n")
	tmp := path + ".tmp"
	tf, err := os.Lstat(tmp)
	if err == nil {
		if !tf.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", tmp)
		}
		if err := os.Remove(tmp); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // G304: exclusive create of the credential temp file
	if err != nil {
		return err
	}
	_, werr := f.Write(content)
	serr := f.Sync()
	cerr := f.Close()
	if werr != nil || serr != nil || cerr != nil {
		_ = os.Remove(tmp)
		if werr != nil {
			return werr
		}
		if serr != nil {
			return serr
		}
		return cerr
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	fi, err = os.Lstat(path)
	if err == nil && (fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular()) {
		_ = os.Remove(tmp)
		return fmt.Errorf("%s is not a regular file", path)
	}
	if err != nil && !os.IsNotExist(err) {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}

func yes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
