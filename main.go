package main

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/urfave/cli"
)

func main() {
	initLogger()

	app := cli.NewApp()
	app.Name = "pocketnpm"
	app.Usage = "A simple but fast npm mirror client & server"
	app.Version = Version

	app.Commands = []cli.Command{
		{
			Name:    "init",
			Aliases: []string{"i"},
			Usage:   "Generate an example config file",
			Flags: []cli.Flag{
				cli.StringFlag{Name: "path, p", Value: "config.toml"},
			},
			Action: func(c *cli.Context) error {
				return nil
			},
		},
		{
			Name:    "run",
			Aliases: []string{"r"},
			Usage:   "Run",
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

				return nil
			},
		},
	}

	app.Run(os.Args)
}
