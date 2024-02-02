package main

import (
	"github.com/omeid/uconfig"
	"github.com/omeid/uconfig/plugins/env"
)

type config struct {
	PrivateKey string `default:""`
	Proof      string `default:""`
	HTTP       struct {
		Port string `default:"8080"`
	}

	Log struct {
		Human bool `default:"false"`
		Debug bool `default:"false"`
	}
}

func initConfig() (*config, error) {
	conf := &config{}
	c, err := uconfig.New(&conf, env.New())
	if err != nil {
		return nil, err
	}

	if err := c.Parse(); err != nil {
		return nil, err
	}

	return conf, nil
}
