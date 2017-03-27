package main

import (
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
