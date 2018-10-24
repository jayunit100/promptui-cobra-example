package apps

// Copyright © 2017 NAME HERE <EMAIL ADDRESS>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

import (
	"bytes"
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
)

func runCmd(cmd string, args ...string) {
	cmd2 := exec.Command(cmd, args...)
	var out bytes.Buffer
	cmd2.Stdout = &out
	cmd2.Run()
	fmt.Printf("%v", out.String())
}

func Run(args []string) error {
	if args == nil || len(args) == 0 {
		return fmt.Errorf("No subcommand provided !")
	} else if args[0] == "status" {
		runCmd("kubectl", "cluster-info")
	} else if args[0] == "list" {
		if args[1] == "hubs" {
			runCmd("kubectl", "get", "hub")
		} else {
			fmt.Println("Error: Did you mean 'list hubs' ?")
		}
	} else {
		return fmt.Errorf("Invalid subcommand provided.")
	}
	return nil
}

var ClusterCommand = &cobra.Command{
	Use:   "cluster",
	Short: "tells you what cluster your on",
	Run: func(cmd *cobra.Command, args []string) {
		if err := Run(args); err != nil {
			cmd.Help()
		}
	},
}

// implementing init is important ! thats how cobra knows to bind your 'app' to top level command.
func init() {
	RootCmd.AddCommand(ClusterCommand)
}
