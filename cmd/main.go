package main

import (
	"fmt"
	"os"

	"github.com/jayunit100/blackduck-ctl/pkg/interactive2"

	"github.com/jayunit100/blackduck-ctl/pkg/apps"
)

func main() {
	if len(os.Args) > 1 {
		if os.Args[1] == "gui" {
			interactive2.Launch()
		}
	} else {
		apps.Execute()
	}
	fmt.Println("Goodbye !")
}
