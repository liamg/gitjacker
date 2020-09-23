package gitjacker

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

var paths = []string{
	"HEAD",
	"objects/info/packs",
	"description",
	"config",
	"COMMIT_EDITMSG",
	"index",
	"packed-refs",
	"refs/heads/master",
	"refs/remotes/origin/HEAD",
	"refs/stash",
	"logs/HEAD",
	"logs/refs/heads/master",
	"logs/refs/remotes/origin/HEAD",
	"info/refs",
	"info/exclude",
}

var ErrNotVulnerable = fmt.Errorf("no .git directory is hosted at this URL")

type retriever struct {
	baseURL    *url.URL
	outputDir  string
	http       *http.Client
	downloaded map[string]bool
}

func New(target *url.URL, outputDir string) *retriever {

	relative, _ := url.Parse(".git/")
	target = target.ResolveReference(relative)

	return &retriever{
		baseURL:   target,
		outputDir: outputDir,
		http: &http.Client{
			Timeout: time.Second * 10,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		downloaded: make(map[string]bool),
	}
}

func (r *retriever) checkVulnerable() error {
	return r.downloadFile("HEAD")
}

func (r *retriever) downloadFile(path string) error {
	if r.downloaded[path] {
		return nil
	}
	r.downloaded[path] = true

	relative, err := url.Parse(path)
	if err != nil {
		return err
	}

	absolute := r.baseURL.ResolveReference(relative)
	resp, err := r.http.Get(absolute.String())
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	filePath := filepath.Join(r.outputDir, ".git", path)
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return err
	}

	return ioutil.WriteFile(filePath, content, 0640)
}

func (r *retriever) Run() error {

	if err := r.checkVulnerable(); err != nil {
		return ErrNotVulnerable
	}

	for _, path := range paths {
		_ = r.downloadFile(path)
	}

	return nil
}
