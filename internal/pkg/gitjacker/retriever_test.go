package gitjacker

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/magiconair/properties/assert"
)

type vulnerableServer struct {
	dir    string
	server *http.Server
}

func newVulnerableServer() (*vulnerableServer, error) {

	dir, err := ioutil.TempDir(os.TempDir(), "gjtest_server")
	if err != nil {
		return nil, err
	}

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	fs := http.FileServer(http.Dir(dir))

	return &vulnerableServer{
		dir: dir,
		server: &http.Server{
			Addr:    "127.0.0.1:9999",
			Handler: fs,
		},
	}, nil
}

func (v *vulnerableServer) Listen() error {
	return v.server.ListenAndServe()
}

func (v *vulnerableServer) Addr() string {
	return v.server.Addr
}

func (v *vulnerableServer) Close() error {
	if err := os.RemoveAll(v.dir); err != nil {
		return err
	}
	return v.server.Close()
}

func (v *vulnerableServer) writeFile(path, content string) error {
	return ioutil.WriteFile(filepath.Join(v.dir, path), []byte(content), 0644)
}

func (v *vulnerableServer) commit(msg string) error {
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = v.dir
	if err := cmd.Run(); err != nil {
		return err
	}

	commitCmd := exec.Command("git", "commit", "-a", "-m", msg)
	commitCmd.Dir = v.dir
	return commitCmd.Run()
}

func TestRetrieval(t *testing.T) {
	server, err := newVulnerableServer()
	if err != nil {
		t.Fatal(err)
	}

	go func() { _ = server.Listen() }()
	defer func() { _ = server.Close() }()

	expectedContent := "<?php\necho 'hello';\n"
	if err := server.writeFile("hello.php", expectedContent); err != nil {
		t.Fatal(err)
	}

	if err := server.commit("first commit"); err != nil {
		t.Fatal(err)
	}

	target, err := url.Parse(fmt.Sprintf("http://%s", server.Addr()))
	if err != nil {
		t.Fatal(err)
	}

	outputDir, err := ioutil.TempDir(os.TempDir(), "gjtest_out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(outputDir) }()

	if err := New(target, outputDir).Run(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "hello.php")); err != nil {
		t.Fatal(err)
	}

	actual, err := ioutil.ReadFile(filepath.Join(outputDir, "hello.php"))
	if err != nil {
		t.Fatal(err)
	}

	assert.Equal(t, string(actual), expectedContent)
}
