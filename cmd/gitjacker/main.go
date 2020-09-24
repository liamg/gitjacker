package main

import (
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/liamg/gitjacker/internal/pkg/gitjacker"
	"github.com/spf13/cobra"
)

var outputDir string

func main() {

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
	RunE: func(cmd *cobra.Command, args []string) error {

		rawURL := args[0]
		rawURL = strings.TrimSuffix(rawURL, "/.git/")
		rawURL = strings.TrimSuffix(rawURL, "/.git")

		u, err := url.Parse(rawURL)
		if err != nil {
			return fmt.Errorf("invalid url: %w", err)
		}

		if !u.IsAbs() {
			return fmt.Errorf("invalid url: must be absolute")
		}

		if outputDir == "" {
			outputDir, err = ioutil.TempDir(os.TempDir(), "gitjacker")
			if err != nil {
				return err
			}
		}

		versionData, err := exec.Command("git", "--version").Output()
		if err != nil {
			return fmt.Errorf("failed to start git: %w - please check it is installed", err)
		}

		version := strings.Split(string(versionData), " ")
		_ = version // TODO output this

		if err := gitjacker.New(u, outputDir).Run(); err != nil {
			return err
		}

		fmt.Printf("Output directory: %s\n", outputDir)

		return nil
	},
}
