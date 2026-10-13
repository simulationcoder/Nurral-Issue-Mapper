package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ".issue-mapper.yml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMissingFileIsFine(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "nope.yml"))
	if err != nil || f.Repo != "" {
		t.Fatalf("f=%+v err=%v", f, err)
	}
}

func TestLoadReadsKeysAndQuotes(t *testing.T) {
	f, err := Load(write(t, "# comment\nrepo: acme/shop\nepic_title: '(?i)^theme:'\nseries_title: \"^(.*) (\\\\d+)/(\\\\d+):\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Repo != "acme/shop" || f.EpicTitle != "(?i)^theme:" {
		t.Fatalf("%+v", f)
	}
	r, err := f.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if !r.EpicTitle.MatchString("Theme: growth") || r.EpicTitle.MatchString("Epic: growth") {
		t.Fatal("custom epic rule not applied")
	}
}

func TestLoadRejectsUnknownKeyAndBadRules(t *testing.T) {
	if _, err := Load(write(t, "colour: blue\n")); err == nil {
		t.Fatal("unknown key should fail")
	}
	if _, err := (File{SeriesTitle: `^(.*) (\d+)`}).Rules(); err == nil {
		t.Fatal("a series rule without three groups should fail")
	}
	if _, err := (File{EpicTitle: `(`}).Rules(); err == nil {
		t.Fatal("an invalid regexp should fail")
	}
}
