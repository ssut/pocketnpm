package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"

	"net/http"
	_ "net/http/pprof"

	"github.com/BurntSushi/toml"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
	"github.com/ssut/pocketnpm/npm"
	"github.com/urfave/cli"
)

func getConfig(path string) *PocketConfig {
	confPath, _ := filepath.Abs(path)
	b, err := ioutil.ReadFile(confPath)
	if err != nil {
		log.Fatal(err)
	}

	var conf PocketConfig
	if _, err := toml.Decode(string(b), &conf); err != nil {
		log.Fatalf("Error in config file: %s", err)

	}

	return &conf
}

func main() {
	log.InitLogger()

	app := cli.NewApp()
	app.Name = "pocketnpm"
	app.Usage = "A simple but fast npm mirror client & server"
	app.Version = Version
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "debug, d"},
		cli.BoolFlag{Name: "profile, p", Usage: "activate pprof on port 18080 for profiling goroutines"},
		cli.IntFlag{Name: "cpus", Value: runtime.NumCPU()},
	}
	app.EnableBashCompletion = true

	app.Before = func(c *cli.Context) error {
		if c.GlobalBool("debug") {
			log.SetDebug()
			log.Debug("Activated debug mode")
		}

		if c.GlobalBool("profile") {
			log.Info("Starting pprof server on port 18080")
			go http.ListenAndServe("localhost:18080", nil)
		}

		cpus := c.GlobalInt("cpus")
		runtime.GOMAXPROCS(cpus)

		return nil
	}

	app.Commands = []cli.Command{
		{
			Name:    "init",
			Aliases: []string{"i"},
			Usage:   "Generate an example config file",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "path, p", Value: "config.toml"},
			},
			Action: func(c *cli.Context) error {
				path, _ := filepath.Abs(c.String("path"))
				out, err := os.Create(path)
				if err != nil {
					log.Fatal(err)
				}
				defer out.Close()

				defaultToml, _ := defaultTomlBytes()
				_, err = out.Write(defaultToml)
				if err != nil {
					log.Fatal(err)
				}

				return nil
			},
		},
		{
			Name:    "start",
			Aliases: []string{"s"},
			Usage:   "Start PocketNPM with continuous mirroring mode",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "config, c", Value: "config.toml"},
				cli.BoolFlag{Name: "only-server", Usage: "Disable mirroring and start only registry server"},
				cli.BoolFlag{Name: "onetime, o", Usage: "One-time mirroring (disable continuous mirroring)"},
				cli.BoolFlag{Name: "server, s", Usage: "Start NPM registry server"},
				cli.BoolFlag{Name: "watch-status, w", Usage: "Watch PocketNPM status"},
			},
			Action: func(c *cli.Context) error {
				conf := getConfig(c.String("config"))

				// global database frontend
				pb := db.NewPocketBase(&conf.DB)

				// database status
				if c.Bool("watch-status") {
					go pb.LogStats()
				}

				// webserver
				if c.Bool("server") {
					server := npm.NewPocketServer(pb, &conf.Server, &conf.Mirror)
					if c.Bool("only-server") {
						server.Run()
						return nil
					}
					go server.Run()
				}

				client := npm.NewMirrorClient(pb, &conf.Mirror)
				client.Run(c.Bool("onetime"))
				return nil
			},
		},
		{
			Name:  "check",
			Usage: "Check consistency between database and on-disk data",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "config, c", Value: "config.toml"},
				cli.BoolFlag{Name: "fix", Usage: "Fix errors (not implemented)"},
			},
			Action: func(c *cli.Context) error {
				conf := getConfig(c.String("config"))

				// global database frontend
				pb := db.NewPocketBase(&conf.DB)
				// database redundancy check
				pb.Check()

				// file check
				client := npm.NewMirrorClient(pb, &conf.Mirror)
				client.Check()

				return nil
			},
		},
	}

	app.Run(os.Args)
}
