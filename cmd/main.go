package main

import (
	"fmt"
	"os"
	"github.com/blackducksoftware/blackduckct-ctl/pkg/interactive"
	"github.com/spf13/viper"
)

type Blackduckctl struct {
	LaunchUI bool
}

func main() {
	viper.ReadInConfig()
	if err := viper.ReadInConfig(); err != nil {
		fmt.Println("Can't read config:", err)
		os.Exit(1)
	}
	config := &Blackduckctl{}
	viper.Unmarshal(config)

	if config.LaunchUI {
		pkg.
	}

}
