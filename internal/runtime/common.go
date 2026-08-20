// Package runtime constructs confined application Podman commands.
package runtime

import (
	"fmt"
	"strings"
)

func containerListen(host string) string {
	if strings.Contains(host, ":") {
		return "::"
	}
	return "0.0.0.0"
}

func publicationNetwork(host string) []string {
	if host == "0.0.0.0" {
		return []string{"--network", "pasta:-4"}
	}
	return nil
}

func publishedPort(host string, port int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s:%d:%d/tcp", host, port, port)
}

func dynamicallyPublishedPort(host string, containerPort int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s::%d/tcp", host, containerPort)
}

func readOnlySharedSuffix(suffix string) string {
	suffix = strings.Replace(suffix, ":rw", ":ro", 1)
	return strings.Replace(suffix, ",Z", ",z", 1)
}

func gpuDeviceArguments(renderNodes []string) []string {
	arguments := []string{"--device", "/dev/kfd"}
	for _, node := range renderNodes {
		arguments = append(arguments, "--device", node)
	}
	return arguments
}

func env(arguments []string, name string, value any) []string {
	return append(arguments, "--env", fmt.Sprintf("%s=%v", name, value))
}
