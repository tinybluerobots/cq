package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v69/github"
)

func TestRender_ExtraFields(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "test.tmpl")

	tmpl := `Repo: {{.Repo}} Author: {{.Author}} Labels: {{.Labels}} Branch: {{.DefaultBranch}}`
	if err := os.WriteFile(tmplPath, []byte(tmpl), 0644); err != nil {
		t.Fatal(err)
	}

	num := 1
	title := "test"
	body := "body"
	login := "octocat"
	user := &github.User{Login: &login}
	labelName := "bug"
	labels := []*github.Label{{Name: &labelName}}

	issue := &github.Issue{
		Number: &num,
		Title:  &title,
		Body:   &body,
		User:   user,
		Labels: labels,
	}

	result, err := Render(tmplPath, "org/repo", issue, "main")
	if err != nil {
		t.Fatal(err)
	}

	if result != "Repo: org/repo Author: octocat Labels: bug Branch: main" {
		t.Errorf("got %q", result)
	}
}

func TestRender_MultipleLabels(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "test.tmpl")

	tmpl := `{{.Labels}}`
	if err := os.WriteFile(tmplPath, []byte(tmpl), 0644); err != nil {
		t.Fatal(err)
	}

	num := 1
	title := "test"
	l1 := "bug"
	l2 := "priority"
	labels := []*github.Label{{Name: &l1}, {Name: &l2}}

	issue := &github.Issue{
		Number: &num,
		Title:  &title,
		Labels: labels,
	}

	result, err := Render(tmplPath, "org/repo", issue, "main")
	if err != nil {
		t.Fatal(err)
	}

	if result != "bug, priority" {
		t.Errorf("got %q", result)
	}
}
