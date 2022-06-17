package main

import (
	"context"
	"docker-utils/utils"
	"fmt"
	"github.com/docker/docker/client"
)

func main() {
	docker, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	networks := utils.ListNetworks(docker, ctx)
	for i, n := range networks {
		fmt.Println(i)
		fmt.Println(n.Name)
	}

	images := utils.ListImages(docker, ctx)

	for i, im := range images {
		fmt.Println(i)
		fmt.Println(im.ID)
		fmt.Println(im.RepoTags)
	}

	containers := utils.ListContainers(docker, ctx)
	for i, c := range containers {
		fmt.Print(i)
		fmt.Print(" | ")
		fmt.Print(c.ID)
		fmt.Print(" | ")
		fmt.Print(c.Image)
		fmt.Print(" | ")
		fmt.Print(c.State)
		fmt.Print(" | ")
		fmt.Print(c.Names)
		fmt.Println("")

	}
}
