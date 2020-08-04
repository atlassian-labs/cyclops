package k8s

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetConfig returns a new kubernetes config
func GetConfig(kubeconfig string) (*rest.Config, error) {
	// If a flag is specified with the config location, use that
	if len(kubeconfig) > 0 {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	// If an env variable is specified with the config location, use that
	if len(os.Getenv("KUBECONFIG")) > 0 {
		return clientcmd.BuildConfigFromFlags("", os.Getenv("KUBECONFIG"))
	}
	// If no explicit location, try the in-cluster config
	if c, err := rest.InClusterConfig(); err == nil {
		return c, nil
	}
	// If no in-cluster config, try the default location in the user's home directory
	if usr, err := user.Current(); err == nil {
		if c, err := clientcmd.BuildConfigFromFlags(
			"", filepath.Join(usr.HomeDir, ".kube", "config")); err == nil {
			return c, nil
		}
	}

	return nil, fmt.Errorf("could not locate a kubeconfig")
}

// NewCLIClientOrDie creates a new controller-runtime client for use with CRDs by wrapping the dynamic.Client
func NewCLIClientOrDie(kubeConfigFlags *genericclioptions.ConfigFlags) client.Client {
	config, err := kubeConfigFlags.ToRESTConfig()
	if err != nil {
		fmt.Println("failed to get k8s config:", err)
		os.Exit(1)
	}
	c, err := client.New(config, client.Options{})
	if err != nil {
		fmt.Println("failed to create k8s client:", err)
		os.Exit(1)
	}

	return c
}
