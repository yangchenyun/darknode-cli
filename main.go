package main

import (
	"log"
	"os"

	"github.com/urfave/cli"
	"os/exec"
	"io/ioutil"
)

func main() {
	// Create new cli application
	app := cli.NewApp()

	// fixme: Define flags
	upFlags := []cli.Flag{
		//cli.StringFlag{
		//	Name:  "name",
		//	Value: "darknode",
		//	Usage: "give your darknode a name",
		//},
		cli.StringFlag{
			Name:  "provider",
			Value: "AWS",
			Usage: "cloud service provider you want to use for your darknode, default to AWS",
		},
		cli.StringFlag{
			Name:  "region",
			Value: "",
			Usage: "deployment region",
		},
		cli.StringFlag{
			Name:  "instance",
			Value: "",
			Usage: "instance type",
		},
		cli.StringFlag{
			Name:  "access_key",
			Value: "",
			Usage: "access key for your AWS account",
		},
		cli.StringFlag{
			Name:  "secret_key",
			Value: "",
			Usage: "secret key for your AWS account",
		},
	}

	// Define sub-commands
	app.Commands = []cli.Command{
		{
			Name:  "up",
			Usage: "deploying a new darknode",
			Flags: upFlags,
			Action: func(c *cli.Context) error {
				//path := fmt.Sprintf("./%v", c.String("name"))
				//if path == "./" {
				//	path = "./darknode"
				//}
				//var err error
				//path, err = Mkdir(path)
				//if err != nil {
				//	return err
				//}

				return deployNode(c)
			},
		},
		{
			Name:  "destroy",
			Usage: "tear down the darknode and clean up everything",
			Action: func(c *cli.Context) error {
				return destroyNode(c)
			},
		},
		{
			Name:  "update",
			Usage: "update your darknode to the latest release",
			Action: func(c *cli.Context) error {
				return updateNode(c)
			},
		},
		{
			Name:  "ssh",
			Usage: "ssh into your cloud service instance",
			Action: func(c *cli.Context) error {
				return sshNode(c)
			},
		},
	}

	// Start the app
	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

// updateNode update the darknode to the latest release from master branch.
// This will restart the darknode.
func updateNode(ctx *cli.Context) error {
	update , err := ioutil.ReadFile("./scripts/update.sh")
	if err != nil {
		return err
	}

	// FIXME : GET IP ADDRESS FROM TERRAFORM OUTPUT
	ip := "ubuntu@52.14.25.55"
	updateCmd := exec.Command("ssh", "-i", "ssh_keypair",ip , string(update))
	pipeToStd(updateCmd)
	if err := updateCmd.Start(); err != nil {
		return err
	}

	return updateCmd.Wait()
}

// sshNode will ssh into the darknode
func sshNode(ctx *cli.Context) error {
	// FIXME : GET IP ADDRESS FROM TERRAFORM OUTPUT
	ip := "ubuntu@52.14.25.55"
	ssh := exec.Command("ssh", "-i", "ssh_keypair",ip )
	pipeToStd(ssh)
	if err := ssh.Start(); err != nil {
		return err
	}

	return ssh.Wait()
}
