// Package config reads .issue-mapper.yml and resolves the repo and token.
package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/simulationcoder/nurral-issue-mapper/internal/graph"
)

// File is the optional .issue-mapper.yml. Every key is optional.
//
//	repo: owner/name
//	epic_title: '(?i)(^|\W)epic(\W|$)'
//	series_title: '^(.*?)\s*\b(\d{1,2})\s*/\s*(\d{1,2})\b\s*[:\-–—]'
type File struct {
	Repo        string
	EpicTitle   string
	SeriesTitle string
}

// Load reads path; a missing file is not an error. The format is the flat
// "key: value" subset of YAML, which is all the file needs.
func Load(path string) (File, error) {
	var f File
	fh, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	defer fh.Close()
	sc := bufio.NewScanner(fh)
	for n := 1; sc.Scan(); n++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		key, val, ok := strings.Cut(text, ":")
		if !ok {
			return f, fmt.Errorf("%s:%d: expected key: value", path, n)
		}
		val = unquote(strings.TrimSpace(val))
		switch strings.TrimSpace(key) {
		case "repo":
			f.Repo = val
		case "epic_title":
			f.EpicTitle = val
		case "series_title":
			f.SeriesTitle = val
		default:
			return f, fmt.Errorf("%s:%d: unknown key %q", path, n, strings.TrimSpace(key))
		}
	}
	return f, sc.Err()
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// Rules applies the file's patterns over the defaults.
func (f File) Rules() (graph.Rules, error) {
	r := graph.DefaultRules()
	if f.EpicTitle != "" {
		re, err := regexp.Compile(f.EpicTitle)
		if err != nil {
			return r, fmt.Errorf("epic_title: %w", err)
		}
		r.EpicTitle = re
	}
	if f.SeriesTitle != "" {
		re, err := regexp.Compile(f.SeriesTitle)
		if err != nil {
			return r, fmt.Errorf("series_title: %w", err)
		}
		if re.NumSubexp() < 3 {
			return r, errors.New("series_title needs three capture groups: label, position, total")
		}
		r.SeriesTitle = re
	}
	return r, nil
}

// Token returns GITHUB_TOKEN / GH_TOKEN, else asks the gh CLI.
func Token() string {
	for _, k := range []string{"GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if out, err := exec.Command("gh", "auth", "token").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	// gh before 2.17 has no `auth token`; its status output can show the token.
	out, _ := exec.Command("gh", "auth", "status", "--show-token").CombinedOutput()
	for _, ln := range strings.Split(string(out), "\n") {
		if _, tok, ok := strings.Cut(ln, "Token:"); ok {
			return strings.TrimSpace(tok)
		}
	}
	return ""
}

var remoteRE = regexp.MustCompile(`github\.com[:/]([^/\s]+)/([^/\s]+?)(?:\.git)?$`)

// RepoFromGit reads owner/name from the origin remote of the current directory.
func RepoFromGit() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	if m := remoteRE.FindStringSubmatch(strings.TrimSpace(string(out))); m != nil {
		return m[1] + "/" + m[2]
	}
	return ""
}
