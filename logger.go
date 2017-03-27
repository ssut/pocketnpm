package main

import (
	"github.com/Sirupsen/logrus"
	prefixed "github.com/x-cray/logrus-prefixed-formatter"
)

var log = logrus.New()

func initLogger() {
	formatter := new(prefixed.TextFormatter)
	log.Formatter = formatter
}
