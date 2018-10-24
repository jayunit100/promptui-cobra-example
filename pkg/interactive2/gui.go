package interactive2

import (
	"fmt"
	"os"

	"github.com/jayunit100/blackduck-ctl/pkg/apps"
	"github.com/manifoldco/promptui"
)

func Launch() {
	for {
		fmt.Println("******************************************")

		prompt := promptui.Select{
			Label: "Select operation",
			Items: []string{
				"opssight",
				"blackduck",
				"api",
				"cluster",
				"EXIT",
			},
		}
		_, result, _ := prompt.Run()
		fmt.Printf("\033[0;0H")

		if result == "cluster" {
			prompt := promptui.Select{
				Label: "Select operation",
				Items: []string{
					"show kube/openshift status",
					"list all hubs",
				},
			}
			_, resultString, _ := prompt.Run()
			if resultString == "show kube/openshift status" {
				apps.ClusterCommand.Run(apps.ClusterCommand, []string{"status"})
			}
			if resultString == "list all hubs" {
				apps.ClusterCommand.Run(apps.ClusterCommand, []string{"list", "hubs"})
			}
		}
		if result == "api" {
			prompt := promptui.Select{
				Label: "Select operation",
				Items: []string{
					"all-components",
					"all-projects",
					"list-hubs",
				},
			}
			prompt.Run()
		}
		if result == "opssight" {
			prompt := promptui.Select{
				Label: "Select operation",
				Items: []string{
					"status",
					"throughput",
					"ns-vuln",
					"deploy",
				},
			}
			prompt.Run()
		}
		if result == "blackduck" {
			prompt := promptui.Select{
				Label: "Select operation",
				Items: []string{
					"status",
					"deploy",
				},
			}
			prompt.Run()
		}
		if result == "EXIT" {
			os.Exit(0)
		}
	}

}
