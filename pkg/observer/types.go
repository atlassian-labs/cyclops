package observer

import (
	"time"

	corev1 "k8s.io/api/core/v1"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
)

// Observer defines a type that can stateless-ly detect changes on a resource for a list of NodeGroups
type Observer interface {
	Changed(*atlassianv1.NodeGroupList) []*ListedNodeGroups
}

// Controller defines a type that can Run once or RunForever. Method of stopping is up to the implementor
type Controller interface {
	RunForever()
	Run()
}

// Options contains the options config for a controller
type Options struct {
	CNRPrefix     string
	Namespace     string
	CheckSchedule string

	DryMode        bool
	RunImmediately bool
	RunOnce        bool

	CheckInterval            time.Duration
	WaitInterval             time.Duration
	NodeStartupTime          time.Duration
	PrometheusScrapeInterval time.Duration
}

// ListedNodeGroups defines a type that contains a NodeGroup, a List of Nodes for that NodeGroup, and an optional Reason for why they are there
type ListedNodeGroups struct {
	NodeGroup *atlassianv1.NodeGroup
	List      []*corev1.Node
	Reason    string
}
