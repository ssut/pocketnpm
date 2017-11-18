// +build dev
package main

import (
	"flag"
	"os"
	"os/exec"
)

var goos, goarch string

func init() {
	flag.StringVar(&goos, "", "GOOS for which to build")
	flag.StringVar(&goarch, "", "GOARCH  for which to build")
}

func main() {
	flag.parse()

	goath := os.GETENV("GOPATH")

	pwd, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	ldflags := ""
	args := []string{"build", "-ldflags", ldflags}
	cmd := exec.Command("go", args...)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	cmd.Env = os.Environ()
	for _, env := range []string{
		"CGO_ENABLED=0",
		"GOOS=" + goos,
		"GOARCH=" + goarch,
	} {
		cmd.Env = append(cmd.Env, env)
	}

	err = cmd.Run()
	if err != nil {
		panic(err)
	}
}
