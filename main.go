package main

import (
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

var (
	VERSION = "SELFBUILD"
)

func getConfig(path string) (*PocketConfig, error) {
	confPath, _ := filepath.Abs(path)
	var conf PocketConfig
	if _, err := toml.DecodeFile(confPath, &conf); err != nil {
		return nil, err
	}

	return &conf, nil
}

func main() {
	log.InitLogger()

	app := cli.NewApp()
	app.Name = "pocketnpm"
	app.Usage = "A simple but fast npm mirror client & server"
	app.Version = VERSION
	app.Flags = []cli.Flag{
		cli.StringFlag{Name: "config, c", Value: "config.toml"},
		cli.BoolFlag{Name: "verbose"},
		cli.BoolFlag{Name: "profile, p", Usage: "activate pprof on port 18080 for profiling goroutines"},
		cli.IntFlag{Name: "cpus", Value: runtime.NumCPU()},
	}
	app.EnableBashCompletion = true

	var config *PocketConfig
	var store *db.Store
	defer func() {
		if store != nil {
			store.Close()
		}
	}()

	app.Before = func(c *cli.Context) error {
		if c.GlobalBool("verbose") {
			log.SetDebug()
			log.Debug("Increase log verbosity")
		}

		if c.GlobalBool("profile") {
			log.Info("Starting pprof server on port 18080")
			go http.ListenAndServe("localhost:18080", nil)
		}

		cpus := c.GlobalInt("cpus")
		runtime.GOMAXPROCS(cpus)

		if cfg := c.GlobalString("config"); cfg != "" {
			log.Info("Loading configurations")
			var err error
			config, err = getConfig(c.GlobalString("config"))
			if err != nil {
				log.Panic(err)
			}

			log.Info("Preparing database")
			store = db.NewStore(&config.DB)
			err = store.Connect()
			if err != nil {
				log.Panic(err)
			}

		} else if cfg == "" {
			log.Warn("Config file not provided")
		}

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
			Name:    "mirror",
			Aliases: []string{"m"},
			Usage:   "Start mirroring",
			Flags: []cli.Flag{
				cli.BoolFlag{Name: "onetime, o", Usage: "One-time mirroring (disable continuous mirroring)"},
			},
			Action: func(c *cli.Context) error {
				client := npm.NewMirrorClient(store, &config.Mirror)
				client.Run(c.Bool("onetime"))
				return nil
			},
		},
		{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "Start PocketNPM server",
			Action: func(c *cli.Context) error {
				server := npm.NewPocketServer(store, &config.Server, &config.Mirror)
				err := server.Run()
				return err
			},
		},
	}

	app.Run(os.Args)
}
