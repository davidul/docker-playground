package utils

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/go-connections/nat"
	"io/ioutil"
	"net/http"
	"time"
)

func main() {

	docker, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	events, errors := docker.Events(ctx, types.EventsOptions{})

	if !findImage(docker, ctx, "my-kafka:latest") {
		fmt.Println("Building image ")
		buildImage(docker, ctx)
	}

	m := make(map[string]string)
	m["com.docker.network.bridge.name"] = "kafka-net"
	m["com.docker.network.bridge.host_binding_ipv4"] = "0.0.0.0"

	list, err := docker.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		panic(err)
	}

	kafkaNetExists := false
	for _, n := range list {
		if n.Name == "kafka-net" {
			kafkaNetExists = true
		}
	}

	if !kafkaNetExists {
		networkCreate, err := docker.NetworkCreate(ctx, "kafka-net", types.NetworkCreate{
			Driver:     "bridge",
			Attachable: true,
			Options:    m,
			Ingress:    false,
			IPAM: &network.IPAM{
				Driver: "default",
				Config: []network.IPAMConfig{
					{Gateway: "172.18.0.1",
						Subnet: "172.18.0.0/16"}},
			},
		})

		if err != nil {
			panic(err)
		}
		fmt.Println(networkCreate)
	}

	kafkaContainerID := createKafkaContainer(docker, ctx)
	zookeeperContainerID := createZookeeperContainer(docker, ctx)

	err = docker.ContainerStart(ctx, zookeeperContainerID, types.ContainerStartOptions{})
	if err != nil {
		panic(err)
	}

	err = docker.ContainerStart(ctx, kafkaContainerID, types.ContainerStartOptions{})
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			select {
			case e := <-errors:
				fmt.Println(e)
			case evt := <-events:
				fmt.Println(evt)
			}
		}
	}()

	singleNodeCB := findContainer(docker, ctx, "/single-node-cb")
	if len(singleNodeCB.ID) == 0 {
		panic("Error")
	}

	duration, _ := time.ParseDuration("10s")

	err = docker.ContainerStop(ctx, singleNodeCB.ID, &duration)
	if err != nil {
		panic(err)
	}

	err = docker.ContainerRemove(ctx, singleNodeCB.ID, types.ContainerRemoveOptions{})
	if err != nil {
		panic(err)
	}

	create, err := docker.ContainerCreate(ctx, &container.Config{
		Image: "couchbase:latest",
		ExposedPorts: nat.PortSet{
			"8091/tcp":  struct{}{},
			"8092/tcp":  struct{}{},
			"8093/tcp":  struct{}{},
			"8094/tcp":  struct{}{},
			"8095/tcp":  struct{}{},
			"8096/tcp":  struct{}{},
			"11210/tcp": struct{}{}},
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			"8091/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "8091"}},
			"8092/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "8092"}},
			"8093/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "8093"}},
			"8094/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "8094"}},
			"8095/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "8095"}},
			"8096/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "8096"}},
			"11210/tcp": []nat.PortBinding{
				{HostIP: "0.0.0.0", HostPort: "11210"}},
		},
	}, nil, nil, "single-node-cb")

	if err != nil {
		panic(err)
	}

	err = docker.ContainerStart(ctx, create.ID, types.ContainerStartOptions{})
	if err != nil {
		panic(err)
	}

	t := findContainer(docker, ctx, "/single-node-cb")

	count := 0

	if t.State != "running" && count < 4 {
		fmt.Println("===========> " + t.Status)
		fmt.Println("===========> " + t.State)
		fmt.Println("", t)
		time.Sleep(10 * time.Second)
		count++
		t = findContainer(docker, ctx, "/single-node-cb")
	}

	fmt.Println("======< ", t)

	for i := 0; i < 4; i++ {
		get, _ := http.Get("http://127.0.0.1:8091/pools")
		/*if err != nil {
		   fmt.Println(get)
		   panic(err)
		}*/

		if get == nil {
			time.Sleep(10 * time.Second)
		}

		if get != nil && get.StatusCode != 200 {
			time.Sleep(10 * time.Second)
		} else {
			break
		}
	}

	config := types.ExecConfig{
		AttachStderr: true,
		AttachStdout: true,
		Tty:          true,
		Cmd: []string{"couchbase-cli", "cluster-init", "-c", "127.0.0.1",
			"--cluster-username", "Administrator",
			"--cluster-password", "password",
			"--services", "data, index, query",
			"--cluster-ramsize", "2048",
			"--cluster-index-ramsize", "1024",
			"--cluster-eventing-ramsize", "512",
			"--index-storage-setting", "default"}}

	execCreate := exec(docker, ctx, create.ID, config)

	inspect, err := docker.ContainerExecInspect(ctx, execCreate.ID)
	if err != nil {
		panic(err)
	}

	fmt.Println(inspect)

	config = types.ExecConfig{
		AttachStderr: true,
		AttachStdout: true,
		Tty:          true,
		Cmd: []string{"couchbase-cli", "bucket-create", "-c", "127.0.0.1",
			"--username", "Administrator",
			"--password", "password",
			"--bucket", "default",
			"--bucket-type", "couchbase",
			"--bucket-ramsize", "1024"}}

	exec(docker, ctx, create.ID, config)

}

func exec(docker *client.Client, ctx context.Context, id string, config types.ExecConfig) types.IDResponse {
	execCreate, err := docker.ContainerExecCreate(ctx, id, config)
	if err != nil {
		panic(err)
	}

	attach, err := docker.ContainerExecAttach(ctx, execCreate.ID, types.ExecStartCheck{Detach: false, Tty: true})

	if err != nil {
		panic(err)
	}
	defer attach.Close()

	err = docker.ContainerExecStart(ctx, execCreate.ID, types.ExecStartCheck{})
	if err != nil {
		panic(err)
	}

	all, err := ioutil.ReadAll(attach.Reader)
	if err != nil {
		panic(err)
	}
	fmt.Println("==================")
	fmt.Println(string(all))

	return execCreate
}

func findContainer(docker *client.Client, ctx context.Context, containerName string) types.Container {
	containerList, err := docker.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		panic(err)
	}

	for _, cnt := range containerList {
		for _, name := range cnt.Names {
			if name == containerName {
				fmt.Println("Found cnt " + name)
				return cnt
			}
		}
	}
	return types.Container{}
}

func CreateContainer(docker *client.Client, ctx context.Context) {

}

func createKafkaContainer(docker *client.Client, ctx context.Context) string {
	t2 := findContainer(docker, ctx, "/my-kafka")

	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{},
	}
	gatewayConfig := &network.EndpointSettings{
		Gateway:   "172.18.0.1",
		IPAddress: "172.18.0.3",
	}

	networkConfig.EndpointsConfig["kafka-net"] = gatewayConfig

	if len(t2.ID) == 0 {
		kafkaContainer, err := docker.ContainerCreate(ctx, &container.Config{
			Image: "my-kafka:latest",
			ExposedPorts: nat.PortSet{
				"9092/tcp": struct{}{}},
		}, &container.HostConfig{
			PortBindings: nat.PortMap{
				"9092/tcp": []nat.PortBinding{
					{HostIP: "0.0.0.0", HostPort: "9092"}},
			},
		},
			networkConfig, nil, "my-kafka",
		)

		if err != nil {
			panic(err)
		}

		return kafkaContainer.ID
	}

	return t2.ID
}

//"2181/tcp": {},
//"2888/tcp": {},
//"3888/tcp": {},
//
//"8080/tcp": {}
func createZookeeperContainer(docker *client.Client, ctx context.Context) string {
	t2 := findContainer(docker, ctx, "/zookeeper")

	fmt.Println("Zookeeper " + t2.ID)

	networkConfig := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{},
	}
	gatewayConfig := &network.EndpointSettings{
		Gateway:   "172.18.0.1",
		IPAddress: "172.18.0.2",
	}

	networkConfig.EndpointsConfig["kafka-net"] = gatewayConfig

	//ExposedPorts: nat.PortSet{
	// "2181/tcp": struct{}{}},
	//}, &container.HostConfig{
	// PortBindings: nat.PortMap{
	//    "2181/tcp": []nat.PortBinding{
	//       {HostIP: "0.0.0.0", HostPort: "2181"}},
	// },
	if len(t2.ID) == 0 {
		zookeeper, err := docker.ContainerCreate(ctx, &container.Config{
			Image: "zookeeper:latest",
		}, &container.HostConfig{},
			networkConfig, nil, "zookeeper",
		)

		if err != nil {
			panic(err)
		}

		return zookeeper.ID
	}

	return t2.ID
}

func buildImage(docker *client.Client, ctx context.Context) {
	/*dockerFile, err := os.Open("C:/Users/davidul/IdeaProjects/_work/_angie_demo/i15n-api-ms/automation/docker/Dockerfile")
	  if err != nil {
	     panic(err)
	  }*/
	/*dockerFileBytes, err := ioutil.ReadAll(dockerFile)
	  if err != nil {
	     panic(err)
	  }*/

	options, err := archive.TarWithOptions("C:\\Users\\davidul\\IdeaProjects\\_work\\_angie_demo\\i15n-api-ms\\_automation\\docker", &archive.TarOptions{})
	if err != nil {
		panic(err)
	}

	_, err = docker.ImageBuild(ctx, options, types.ImageBuildOptions{
		Dockerfile: "Dockerfile",
		Tags:       []string{"my-kafka"},
	})

	if err != nil {
		panic(err)
	}
}

func findImage(docker *client.Client, ctx context.Context, imageName string) bool {
	list, err := docker.ImageList(ctx, types.ImageListOptions{All: true})
	if err != nil {
		panic(err)
	}

	for _, image := range list {
		for _, repo := range image.RepoTags {
			if repo == imageName {
				fmt.Println("Found image " + imageName)
				return true
			}
		}
	}

	return false
}

func ListNetworks(docker *client.Client, ctx context.Context) []types.NetworkResource {
	list, err := docker.NetworkList(ctx, types.NetworkListOptions{})
	if err != nil {
		panic(err)
	}

	return list
}

func ListImages(docker *client.Client, ctx context.Context) []types.ImageSummary {
	list, err := docker.ImageList(ctx, types.ImageListOptions{All: true})
	if err != nil {
		panic(err)
	}

	return list
}

func ListContainers(docker *client.Client, ctx context.Context) []types.Container {
	list, err := docker.ContainerList(ctx, types.ContainerListOptions{All: true})
	if err != nil {
		panic(err)
	}

	return list
}
