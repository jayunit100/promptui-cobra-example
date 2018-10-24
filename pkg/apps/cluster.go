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

var clusterCommand = &cobra.Command{
	Use:   "cluster",
	Short: "tells you what cluster your on",
	Run: func(cmd *cobra.Command, args []string) {
		cmd2 := exec.Command("kubectl", "cluster-info")
		var out bytes.Buffer
		cmd2.Stdout = &out
		cmd2.Run()
		fmt.Printf("*** \n %v \n ****", out.String())
	},
}

// implementing init is important ! thats how cobra knows to bind your 'app' to top level command.
func init() {
	RootCmd.AddCommand(clusterCommand)

	// specific flags for your app, add them here...
	clusterCommand.Flags().BoolP("omg", "o", false, "oh wow ")
}
