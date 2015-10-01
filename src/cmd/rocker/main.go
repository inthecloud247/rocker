/*-
 * Copyright 2015 Grammarly, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rocker/build"
	"rocker/dockerclient"
	"rocker/template"

	"github.com/codegangsta/cli"
	"github.com/docker/docker/pkg/units"
	"github.com/fatih/color"
	"github.com/fsouza/go-dockerclient"

	log "github.com/Sirupsen/logrus"
)

var (
	// Version that is passed on compile time through -ldflags
	Version = "built locally"

	// GitCommit that is passed on compile time through -ldflags
	GitCommit = "none"

	// GitBranch that is passed on compile time through -ldflags
	GitBranch = "none"

	// BuildTime that is passed on compile time through -ldflags
	BuildTime = "none"
)

func init() {
	log.SetOutput(os.Stdout)
	log.SetLevel(log.InfoLevel)
}

func main() {
	app := cli.NewApp()

	app.Name = "rocker"
	app.Version = fmt.Sprintf("%s - %.7s (%s) %s", Version, GitCommit, GitBranch, BuildTime)

	app.Usage = "Docker based build tool\n\n   Run 'rocker COMMAND --help' for more information on a command."

	app.Author = ""
	app.Email = ""
	app.Authors = []cli.Author{
		{"Yura Bogdanov", "yuriy.bogdanov@grammarly.com"},
		{"Stas Levental", "stas.levental@grammarly.com"},
	}

	app.Flags = append([]cli.Flag{
		cli.BoolFlag{
			Name: "verbose, vv, D",
		},
		cli.BoolFlag{
			Name: "json",
		},
		cli.BoolTFlag{
			Name: "colors",
		},
	}, dockerclient.GlobalCliParams()...)

	buildFlags := []cli.Flag{
		cli.StringFlag{
			Name:  "file, f",
			Value: "Rockerfile",
			Usage: "rocker build file to execute",
		},
		cli.StringFlag{
			Name:  "auth, a",
			Value: "",
			Usage: "Username and password in user:password format",
		},
		cli.StringSliceFlag{
			Name:  "var",
			Value: &cli.StringSlice{},
			Usage: "set variables to pass to build tasks, value is like \"key=value\"",
		},
		cli.BoolFlag{
			Name:  "no-cache",
			Usage: "supresses cache for docker builds",
		},
		cli.BoolFlag{
			Name:  "no-reuse",
			Usage: "suppresses reuse for all the volumes in the build",
		},
		cli.BoolFlag{
			Name:  "push",
			Usage: "pushes all the images marked with push to docker hub",
		},
		cli.BoolFlag{
			Name:  "pull",
			Usage: "always attempt to pull a newer version of the FROM images",
		},
		cli.BoolFlag{
			Name:  "attach",
			Usage: "attach to a container in place of ATTACH command",
		},
		cli.BoolFlag{
			Name:  "meta",
			Usage: "add metadata to the tagged images, such as user, Rockerfile source, variables and git branch/sha",
		},
		cli.BoolFlag{
			Name:  "print",
			Usage: "just print the Rockerfile after template processing and stop",
		},
		cli.StringFlag{
			Name:  "id",
			Usage: "override the default id generation strategy for current build",
		},
		cli.StringFlag{
			Name:  "artifacts-path",
			Usage: "put artifacts (files with pushed images description) to the directory",
		},
		cli.BoolFlag{
			Name:  "no-garbage",
			Usage: "remove the images from the tail if not tagged",
		},
	}

	app.Commands = []cli.Command{
		{
			Name:   "build",
			Usage:  "launches a build for the specified Rockerfile",
			Action: buildCommand,
			Flags:  buildFlags,
		},
		{
			Name:   "clean",
			Usage:  "complete a task on the list",
			Action: cleanCommand,
		},
		dockerclient.InfoCommandSpec(),
	}

	app.CommandNotFound = func(ctx *cli.Context, command string) {
		fmt.Printf("Command not found: %v\n", command)
		os.Exit(1)
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Printf(err.Error())
		os.Exit(1)
	}
}

func buildCommand(c *cli.Context) {

	var (
		rockerfile *build.Rockerfile
		err        error
	)

	initLogs(c)

	cliVars, err := template.VarsFromStrings(c.StringSlice("var"))
	if err != nil {
		log.Fatal(err)
	}

	vars := template.Vars{}.Merge(cliVars)

	// obtain git info about current directory
	// gitInfo, err := git.Info(filepath.Dir(configFilename))
	// if err != nil {
	// 	// Ignore if given directory is not a git repo
	// 	if _, ok := err.(*git.ErrNotGitRepo); !ok {
	// 		log.Fatal(err)
	// 	}
	// }

	// // some additional useful vars
	// vars["commit"] = stringOr(os.Getenv("GIT_COMMIT"), gitInfo.Sha)
	// vars["branch"] = stringOr(os.Getenv("GIT_BRANCH"), gitInfo.Branch)
	// vars["git_url"] = stringOr(os.Getenv("GIT_URL"), gitInfo.URL)
	// vars["commit_message"] = gitInfo.Message
	// vars["commit_author"] = gitInfo.Author

	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	configFilename := c.String("file")
	contextDir := wd

	if configFilename == "-" {

		rockerfile, err = build.NewRockerfile(filepath.Base(wd), os.Stdin, vars, template.Funs{})
		if err != nil {
			log.Fatal(err)
		}

	} else {

		if !filepath.IsAbs(configFilename) {
			configFilename = filepath.Join(wd, configFilename)
		}

		rockerfile, err = build.NewRockerfileFromFile(configFilename, vars, template.Funs{})
		if err != nil {
			log.Fatal(err)
		}

		// Initialize context dir
		contextDir = filepath.Dir(configFilename)
		args := c.Args()
		if len(args) > 0 {
			contextDir = args[0]
			if !filepath.IsAbs(contextDir) {
				contextDir = filepath.Join(wd, args[0])
			}
		}
	}

	if c.Bool("print") {
		fmt.Print(rockerfile.Content)
		os.Exit(0)
	}

	dockerignore := []string{}

	dockerignoreFilename := filepath.Join(contextDir, ".dockerignore")
	if _, err := os.Stat(dockerignoreFilename); err == nil {
		if dockerignore, err = build.ReadDockerignoreFile(dockerignoreFilename); err != nil {
			log.Fatal(err)
		}
	}

	dockerClient, err := dockerclient.NewFromCli(c)
	if err != nil {
		log.Fatal(err)
	}

	auth := docker.AuthConfiguration{}
	authParam := c.String("auth")
	if strings.Contains(authParam, ":") {
		userPass := strings.Split(authParam, ":")
		auth.Username = userPass[0]
		auth.Password = userPass[1]
	}

	client := build.NewDockerClient(dockerClient, auth)

	var cache build.Cache
	if !c.Bool("no-cache") {
		// TODO: configurable cache dir
		cache = build.NewCacheFS(os.Getenv("HOME") + "/.rocker_cache")
	}

	builder := build.New(client, rockerfile, cache, build.Config{
		InStream:      os.Stdin,
		OutStream:     os.Stdout,
		ContextDir:    contextDir,
		Dockerignore:  dockerignore,
		ArtifactsPath: c.String("artifacts-path"),
		Pull:          c.Bool("pull"),
		NoGarbage:     c.Bool("no-garbage"),
		Attach:        c.Bool("attach"),
		Verbose:       c.GlobalBool("verbose"),
		ID:            c.String("id"),
		NoCache:       c.Bool("no-cache"),
		Push:          c.Bool("push"),
	})

	plan, err := build.NewPlan(rockerfile.Commands(), true)
	if err != nil {
		log.Fatal(err)
	}

	if err := builder.Run(plan); err != nil {
		log.Fatal(err)
	}

	size := fmt.Sprintf("final size %s (+%s from the base image)",
		units.HumanSize(float64(builder.VirtualSize)),
		units.HumanSize(float64(builder.ProducedSize)),
	)

	log.Infof("Successfully built %.12s | %s", builder.GetImageID(), size)

	// builder := build.Builder{
	// 	Rockerfile:   configFilename,
	// 	ContextDir:   contextDir,
	// 	UtilizeCache: !c.Bool("no-cache"),
	// 	Push:         c.Bool("push"),
	// 	NoReuse:      c.Bool("no-reuse"),
	// 	Verbose:      c.Bool("verbose"),
	// 	Attach:       c.Bool("attach"),
	// 	Print:        c.Bool("print"),
	// 	Auth:         auth,
	// 	Vars:         vars,
	// 	CliVars:      cliVars,
	// 	InStream:     os.Stdin,
	// 	OutStream:    os.Stdout,
	// 	Docker:       dockerClient,
	// 	AddMeta:      c.Bool("meta"),
	// 	Pull:         c.Bool("pull"),
	// 	ID:           c.String("id"),
	// }

	// if _, err := builder.Build(); err != nil {
	// 	log.Fatal(err)
	// }
}

func cleanCommand(c *cli.Context) {
	verbose := c.Bool("verbose")
	fmt.Println("verbose")
	fmt.Println(verbose)
}

func initLogs(ctx *cli.Context) {
	logger := log.StandardLogger()

	if ctx.GlobalBool("verbose") {
		logger.Level = log.DebugLevel
	}

	var (
		isTerm    = log.IsTerminal()
		json      = ctx.GlobalBool("json")
		useColors = isTerm && !json
	)

	if ctx.GlobalIsSet("colors") {
		useColors = ctx.GlobalBool("colors")
	}

	color.NoColor = !useColors

	if json {
		logger.Formatter = &log.JSONFormatter{}
	} else {
		formatter := &build.TextFormatter{}
		formatter.DisableColors = !useColors

		logger.Formatter = formatter
	}
}

func stringOr(args ...string) string {
	for _, str := range args {
		if str != "" {
			return str
		}
	}
	return ""
}
