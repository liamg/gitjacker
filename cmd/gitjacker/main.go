package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"

	"github.com/liamg/gitjacker/internal/app/version"
	"github.com/liamg/gitjacker/internal/pkg/gitjacker"
	"github.com/liamg/tml"
	"github.com/spf13/cobra"
)

var outputDir string
var verbose bool

func main() {

	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", verbose, "Enable verbose logging")
	rootCmd.Flags().StringVarP(&outputDir, "output-dir", "o", outputDir, "Directory to output retrieved git repository - defaults to a temporary directory")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	SilenceUsage: true,
	Use:          "gitjacker [url]",
	Short:        "Gitjacker steals git repositories from websites which mistakenly host the contents of the .git directory",
	Long: `Gitjacker steals git repositories from websites which mistakenly host the contents of the .git directory.
More information at https://github.com/liamg/gitjacker`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {

		_ = tml.Printf(`<red>
 ██████  ██ ████████   ██  █████   ██████ ██   ██ ███████ ██████  
██       ██    ██      ██ ██   ██ ██      ██  ██  ██      ██   ██ 
██   ███ ██    ██      ██ ███████ ██      █████   █████   ██████  
██    ██ ██    ██ ██   ██ ██   ██ ██      ██  ██  ██      ██   ██ 
 ██████  ██    ██  █████  ██   ██  ██████ ██   ██ ███████ ██   ██
https://github.com/liamg/gitjacker                      %9s
`, version.Version)

		rawURL := args[0]
		rawURL = strings.TrimSuffix(rawURL, "/.git/")
		rawURL = strings.TrimSuffix(rawURL, "/.git")

		u, err := url.Parse(rawURL)
		if err != nil {
			fail("Invalid url: %s", err)
		}

		if !u.IsAbs() {
			fail("Invalid url: must be absolute e.g. https://victim.website/")
		}

		if outputDir == "" {
			outputDir, err = ioutil.TempDir(os.TempDir(), "gitjacker")
			if err != nil {
				fail("Failed to create temporary directory: %s", err)
			}
		}

		versionData, err := exec.Command("git", "--version").Output()
		if err != nil {
			fail("Cannot check git version: %s - please check it is installed", err)
		}
		versionParts := strings.Split(string(versionData), " ")
		version := strings.TrimSpace(versionParts[len(versionParts)-1])

		if verbose {
			logrus.SetLevel(logrus.DebugLevel)
		}

		_ = tml.Printf(`
Target:     <yellow>%s</yellow>
Local Git:  %s
Output Dir: %s
`, u.String(), version, outputDir)

		if !verbose {
			_ = tml.Printf("\n<yellow>Gitjacking in progress...")
		}

		summary, err := gitjacker.New(u, outputDir).Run()
		if err != nil {
			if !verbose {
				fmt.Printf("\x1b[2K\r")
			}
			if errors.Is(err, gitjacker.ErrNotVulnerable) {
				fail("The provided URL does not appear vulnerable.\n\nError: %s", err)
			}
			fail("Gitjacking failed: %s", err)
		}

		if !verbose {
			_ = tml.Printf("\x1b[2K\r<yellow>Operation complete.\n")
		}

		status := "FAILED"
		switch summary.Status {
		case gitjacker.StatusPartialSuccess:
			status = tml.Sprintf("<yellow>Partial Success")
		case gitjacker.StatusSuccess:
			status = tml.Sprintf("<green>Success")
		}

		var remoteStr string
		for _, remote := range summary.Config.Remotes {
			remoteStr = fmt.Sprintf("%s\n  - %s: %s", remoteStr, remote.Name, remote.URL)
		}

		var branchStr string
		for _, branch := range summary.Config.Branches {
			branchStr = fmt.Sprintf("%s\n  - %s (%s)", branchStr, branch.Name, branch.Remote)
		}

		_ = tml.Printf(`
Status:            %s
Retrieved Objects: <green>%d</green>
Missing Objects:   <red>%d</red>
Pack Data Listed:  %t
Repository:        %s
Remotes:           %s
Branches:          %s

You can find the retrieved repository data in <blue>%s</blue>

`,
			status,
			len(summary.FoundObjects),
			len(summary.MissingObjects),
			summary.PackInformationAvailable,
			summary.Config.RepositoryName,
			remoteStr,
			branchStr,
			summary.OutputDirectory,
		)
	},
}

func fail(format string, args ...interface{}) {
	_, _ = fmt.Fprintln(os.Stderr, tml.Sprintf("<red>%s", fmt.Sprintf(format, args...)))
	os.Exit(1)
}
