package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ssut/pocketnpm/db"
	"github.com/ssut/pocketnpm/log"
	"github.com/ssut/pocketnpm/npm"
	"github.com/urfave/cli"
)

func main() {
	log.InitLogger()

	app := cli.NewApp()
	app.Name = "pocketnpm"
	app.Usage = "A simple but fast npm mirror client & server"
	app.Version = Version
	app.Flags = []cli.Flag{
		cli.BoolFlag{Name: "debug, d"},
	}
	app.EnableBashCompletion = true
	app.BashComplete = func(c *cli.Context) {
		fmt.Fprintf(c.App.Writer, "init\nmirror\nserve\n")
	}

	app.Before = func(c *cli.Context) error {
		if c.GlobalBool("debug") {
			log.SetDebug()
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
			Usage:   "Run mirroring process",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "config, c", Value: "config.toml"},
			},
			Action: func(c *cli.Context) error {
				confPath, _ := filepath.Abs(c.String("config"))
				b, err := ioutil.ReadFile(confPath)
				if err != nil {
					log.Fatal(err)
				}

				var conf PocketConfig
				if _, err := toml.Decode(string(b), &conf); err != nil {
					log.Fatalf("Error in config file: %s", err)
				}

				pb := db.NewPocketBase(&conf.DB)
				client := npm.NewMirrorClient(pb, &conf.Mirror)
				client.Run()
				return nil
			},
		},
	}

	app.Run(os.Args)
}
