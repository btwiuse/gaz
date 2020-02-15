/* Copyright 2017 The Bazel Authors. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/bazelbuild/bazel-gazelle/config"
	"github.com/bazelbuild/bazel-gazelle/repo"
	"github.com/bazelbuild/bazel-gazelle/rule"
	"github.com/btwiuse/pretty"
)

type goRepository struct {
	Name       string
	ImportPath string
	Sum        string
	Version    string
}

type updateReposConfig struct {
	repoFilePath  string
	importPaths   []string
	macroFileName string
	macroDefName  string
	pruneRules    bool
	workspace     *rule.File
	repoFileMap   map[string]*rule.File
}

const updateReposName = "_update-repos"

func getUpdateReposConfig(c *config.Config) *updateReposConfig {
	return c.Exts[updateReposName].(*updateReposConfig)
}

type updateReposConfigurer struct{}

type macroFlag struct {
	macroFileName *string
	macroDefName  *string
}

func (f macroFlag) Set(value string) error {
	args := strings.Split(value, "%")
	if len(args) != 2 {
		return fmt.Errorf("Failure parsing to_macro: %s, expected format is macroFile%%defName", value)
	}
	if strings.HasPrefix(args[0], "..") {
		return fmt.Errorf("Failure parsing to_macro: %s, macro file path %s should not start with \"..\"", value, args[0])
	}
	*f.macroFileName = args[0]
	*f.macroDefName = args[1]
	return nil
}

func (f macroFlag) String() string {
	return ""
}

func (*updateReposConfigurer) RegisterFlags(fs *flag.FlagSet, cmd string, c *config.Config) {
	uc := &updateReposConfig{}
	c.Exts[updateReposName] = uc
	fs.StringVar(&uc.repoFilePath, "from_file", "", "Gazelle will translate repositories listed in this file into repository rules in WORKSPACE or a .bzl macro function. Gopkg.lock and go.mod files are supported")
	fs.Var(macroFlag{macroFileName: &uc.macroFileName, macroDefName: &uc.macroDefName}, "to_macro", "Tells Gazelle to write repository rules into a .bzl macro function rather than the WORKSPACE file. . The expected format is: macroFile%defName")
	fs.BoolVar(&uc.pruneRules, "prune", false, "When enabled, Gazelle will remove rules that no longer have equivalent repos in the Gopkg.lock/go.mod file. Can only used with -from_file.")
}

func (*updateReposConfigurer) CheckFlags(fs *flag.FlagSet, c *config.Config) error {
	uc := getUpdateReposConfig(c)
	switch {
	case uc.repoFilePath != "":
		if len(fs.Args()) != 0 {
			return fmt.Errorf("got %d positional arguments with -from_file; wanted 0.\nTry -help for more information.", len(fs.Args()))
		}

	default:
		if len(fs.Args()) == 0 {
			return fmt.Errorf("no repositories specified\nTry -help for more information.")
		}
		if uc.pruneRules {
			return fmt.Errorf("the -prune option can only be used with -from_file")
		}
		uc.importPaths = fs.Args()
	}

	var err error
	workspacePath := filepath.Join(c.RepoRoot, "WORKSPACE")
	uc.workspace, err = rule.LoadWorkspaceFile(workspacePath, "")
	if err != nil {
		return fmt.Errorf("loading WORKSPACE file: %v", err)
	}
	c.Repos, uc.repoFileMap, err = repo.ListRepositories(uc.workspace)
	if err != nil {
		return fmt.Errorf("loading WORKSPACE file: %v", err)
	}

	return nil
}

func (*updateReposConfigurer) KnownDirectives() []string { return nil }

func (*updateReposConfigurer) Configure(c *config.Config, rel string, f *rule.File) {}

func updateRepos(args []string) (err error) {
	log.Println("[DO] gazelle update-repos", args)
	defer log.Println("[DONE] gazelle update-repos", args)
	// Build configuration with all languages.
	cexts := make([]config.Configurer, 0, len(languages)+2)
	cexts = append(cexts, &config.CommonConfigurer{}, &updateReposConfigurer{})
	kinds := make(map[string]rule.KindInfo)
	loads := []rule.LoadInfo{}
	for _, lang := range languages {
		cexts = append(cexts, lang)
		loads = append(loads, lang.Loads()...)
		for kind, info := range lang.Kinds() {
			kinds[kind] = info
		}
	}
	c, err := newUpdateReposConfiguration(args, cexts)
	if err != nil {
		return err
	}
	// uc := getUpdateReposConfig(c)

	// TODO(jayconrod): move Go-specific RemoteCache logic to language/go.
	var knownRepos []repo.Repo
	for _, r := range c.Repos {
		if r.Kind() == "go_repository" {
			gr := &goRepository{
				Name:       r.AttrString("name"),
				ImportPath: r.AttrString("importpath"),
				Sum:        r.AttrString("sum"),
				Version:    goVersion(r.AttrString("version")),
			}
			kr := repo.Repo{
				Name:     r.Name(),
				GoPrefix: r.AttrString("importpath"),
				Remote:   r.AttrString("remote"),
				VCS:      r.AttrString("vcs"),
			}
			knownRepos = append(knownRepos, kr)
			pretty.JSON(gr)
		}
	}

	return nil
}

func goVersion(in string) (out string) {
	parts := strings.Split(in, "-")
	out = parts[len(parts)-1]
	return
}
func newUpdateReposConfiguration(args []string, cexts []config.Configurer) (*config.Config, error) {
	c := config.New()
	fs := flag.NewFlagSet("gazelle", flag.ContinueOnError)
	// Flag will call this on any parse error. Don't print usage unless
	// -h or -help were passed explicitly.
	fs.Usage = func() {}
	for _, cext := range cexts {
		cext.RegisterFlags(fs, "update-repos", c)
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			updateReposUsage(fs)
			return nil, err
		}
		// flag already prints the error; don't print it again.
		return nil, errors.New("Try -help for more information")
	}
	for _, cext := range cexts {
		if err := cext.CheckFlags(fs, c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

func updateReposUsage(fs *flag.FlagSet) {
	fmt.Fprint(os.Stderr, `usage:

# Add/update repositories by import path
gazelle update-repos example.com/repo1 example.com/repo2

# Import repositories from lock file
gazelle update-repos -from_file=file

The update-repos command updates repository rules in the WORKSPACE file.
update-repos can add or update repositories explicitly by import path.
update-repos can also import repository rules from a vendoring tool's lock
file (currently only deps' Gopkg.lock is supported).

FLAGS:

`)
	fs.PrintDefaults()
}
