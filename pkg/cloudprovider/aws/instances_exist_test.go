package aws

import (
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/stretchr/testify/assert"

	fakeaws "github.com/atlassian-labs/cyclops/pkg/cloudprovider/aws/fake"
)

func TestInstancesExist(t *testing.T) {
	fakeEc2 := &fakeaws.Ec2{Instances: map[string]*fakeaws.Instance{
		"i-0a1b2c3d4e5f60001": {InstanceID: "i-0a1b2c3d4e5f60001", State: ec2.InstanceStateNameRunning},
		"i-0a1b2c3d4e5f60002": {InstanceID: "i-0a1b2c3d4e5f60002", State: ec2.InstanceStateNameTerminated},
	}}
	p := &provider{ec2Service: fakeEc2}

	running := fakeaws.GenerateProviderID("i-0a1b2c3d4e5f60001")
	terminated := fakeaws.GenerateProviderID("i-0a1b2c3d4e5f60002")
	// EC2 stops reporting terminated instances after a while.
	expired := fakeaws.GenerateProviderID("i-0a1b2c3d4e5f60003")

	existing, err := p.InstancesExist([]string{running, terminated, expired})

	assert.NoError(t, err)
	assert.Equal(t, map[string]interface{}{running: nil}, existing)
}

func TestInstancesExistOverFilterLimit(t *testing.T) {
	fakeEc2 := &fakeaws.Ec2{Instances: map[string]*fakeaws.Instance{}}
	var providerIDs []string
	for i := 0; i < 2*maxFilterValues+1; i++ {
		id := fmt.Sprintf("i-%017x", i)
		fakeEc2.Instances[id] = &fakeaws.Instance{InstanceID: id, State: ec2.InstanceStateNameRunning}
		providerIDs = append(providerIDs, fakeaws.GenerateProviderID(id))
	}
	p := &provider{ec2Service: fakeEc2}

	existing, err := p.InstancesExist(providerIDs)

	assert.NoError(t, err)
	assert.Len(t, existing, len(providerIDs))
}

func TestInstancesExistRejectsMalformedInstanceID(t *testing.T) {
	p := &provider{ec2Service: &fakeaws.Ec2{Instances: map[string]*fakeaws.Instance{}}}

	_, err := p.InstancesExist([]string{"aws:///us-east-1a/not-an-instance"})

	assert.Error(t, err)
}
