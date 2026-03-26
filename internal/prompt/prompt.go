package prompt

import (
	"bytes"
	_ "embed"
	"os"
	"strings"
	"text/template"

	"github.com/google/go-github/v69/github"
)

//go:embed default.tmpl
var defaultTemplate string

// Data holds the template fields available to the prompt.
type Data struct {
	Repo          string
	Number        int
	Title         string
	Body          string
	Author        string
	Labels        string
	DefaultBranch string
}

// EnsureFile writes the default prompt template to path if it doesn't exist.
func EnsureFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}

	return os.WriteFile(path, []byte(defaultTemplate), 0644)
}

// Render loads the template from path and renders it with the given issue data.
func Render(path, repo string, issue *github.Issue, defaultBranch string) (string, error) {
	tmplBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("prompt").Parse(string(tmplBytes))
	if err != nil {
		return "", err
	}

	body := ""
	if issue.Body != nil {
		body = *issue.Body
	}

	author := ""
	if issue.User != nil {
		author = issue.User.GetLogin()
	}

	var labelNames []string
	for _, l := range issue.Labels {
		labelNames = append(labelNames, l.GetName())
	}

	data := Data{
		Repo:          repo,
		Number:        issue.GetNumber(),
		Title:         issue.GetTitle(),
		Body:          body,
		Author:        author,
		Labels:        strings.Join(labelNames, ", "),
		DefaultBranch: defaultBranch,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
