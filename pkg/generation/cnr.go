package generation

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"

	atlassianv1 "github.com/atlassian-labs/cyclops/pkg/apis/atlassian/v1"
	"github.com/atlassian-labs/cyclops/pkg/k8s"
)

// ListCNRs list CNRs from ListOptions
func ListCNRs(c client.Client, options *client.ListOptions) (*atlassianv1.CycleNodeRequestList, error) {
	var list atlassianv1.CycleNodeRequestList

	err := c.List(context.TODO(), &list, options)
	if err != nil {
		return nil, err
	}

	return &list, nil
}

// ApplyCNR takes a cnr and optionally uses dry mode in the create request
func ApplyCNR(c client.Client, drymode bool, cnr atlassianv1.CycleNodeRequest) error {
	var dryruns []string
	if drymode {
		dryruns = []string{"All"}
	}
	createOptions := &client.CreateOptions{
		DryRun: dryruns,
	}
	return c.Create(context.TODO(), &cnr, createOptions)
}

// ValidateCNR determines if a cnr should be applied to the cluster or not, and if so why not
func ValidateCNR(nodeLister k8s.NodeLister, cnr atlassianv1.CycleNodeRequest) (bool, string) {
	if ok, reason := validateMetadata(cnr.ObjectMeta); !ok {
		return ok, reason
	}

	if ok, reason := validateCycleSettings(cnr.Spec.CycleSettings); !ok {
		return ok, reason
	}

	// Protect against failure case where cyclops checks for leftover CycleNodeStatus objects using the CycleNodeRequest name in the label selector
	// Label values must be no more than 63 characters long
	name, suffix := GetNameExample(cnr.ObjectMeta)
	validationErrors := validation.IsDNS1035Label(name + suffix)

	if len(validationErrors) > 0 {
		return false, strings.Join(validationErrors, ",")
	}

	// validate against nodes in api
	selector, err := cnr.NodeLabelSelector()
	if err != nil {
		return false, fmt.Sprint("failed to parse node label selectors: ", err.Error())
	}

	return validateSelectorWithNodes(nodeLister, selector, cnr.Spec.NodeNames)
}

// GiveReason adds a reason annotation to the cnr
func GiveReason(cnr *atlassianv1.CycleNodeRequest, reason string) {
	if cnr.Annotations == nil {
		cnr.Annotations = map[string]string{}
	}
	cnr.Annotations[cnrReasonAnnotationKey] = reason
}

// GenerateCNR creates a setup CNR from a NodeGroup with the specified params
func GenerateCNR(nodeGroup atlassianv1.NodeGroup, nodes []string, name, namespace string) atlassianv1.CycleNodeRequest {
	finalName := fmt.Sprintf("%s-%s", name, nodeGroup.Name)
	if name == "" {
		finalName = nodeGroup.Name
	}

	var labels map[string]string
	if name != "" {
		labels = map[string]string{
			cnrNameLabelKey: name,
		}
	}

	return atlassianv1.CycleNodeRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      finalName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: atlassianv1.CycleNodeRequestSpec{
			NodeGroupsList:           nodeGroup.GetNodeGroupNames(),
			Selector:                 nodeGroup.Spec.NodeSelector,
			NodeNames:                nodes,
			CycleSettings:            nodeGroup.Spec.CycleSettings,
			HealthChecks:             nodeGroup.Spec.HealthChecks,
			PreTerminationChecks:     nodeGroup.Spec.PreTerminationChecks,
			SkipInitialHealthChecks:  nodeGroup.Spec.SkipInitialHealthChecks,
			SkipPreTerminationChecks: nodeGroup.Spec.SkipPreTerminationChecks,
		},
	}
}

// UseGenerateNameCNR swaps name with generate name appending the "-" and blanks out Name
func UseGenerateNameCNR(cnr *atlassianv1.CycleNodeRequest) {
	cnr.GenerateName = fmt.Sprintf("%s-", cnr.Name)
	cnr.Name = ""
}
