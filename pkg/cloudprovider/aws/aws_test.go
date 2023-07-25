package aws

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
)

type mockedEC2 struct {
	ec2iface.EC2API
	Resp ec2.DescribeLaunchTemplateVersionsOutput
}

func (m mockedEC2) DescribeLaunchTemplateVersions(in *ec2.DescribeLaunchTemplateVersionsInput) (*ec2.DescribeLaunchTemplateVersionsOutput, error) {
	// Only need to return mocked response output
	return &m.Resp, nil
}

// Test_providerIDToInstanceID is checking that the regex used is correctly matching the providerID to instanceID format
// rather than ensuring the correct instanceID format exactly
func Test_providerIDToInstanceID(t *testing.T) {
	tests := []struct {
		name       string
		providerID string
		instanceID string
		wantErr    bool
	}{
		{
			"expected format",
			"aws:///us-west-2b/i-0bdf741206dd9793c",
			"i-0bdf741206dd9793c",
			false,
		},
		{
			"incorrect format. missing 3rd /",
			"aws://us-west-2b/i-0bdf741206dd9793c",
			"i-0bdf741206dd9793c",
			true,
		},
		{
			"incorrect format. missing id",
			"aws://us-west-2b/",
			"i-0bdf741206dd9793c",
			true,
		},
		{
			"incorrect format. missing numbers",
			"aws://us-west-2b/i-",
			"i-0bdf741206dd9793c",
			true,
		},
		{
			"incorrect format. missing i",
			"aws://us-west-2b/0bdf741206dd9793c",
			"i-0bdf741206dd9793c",
			true,
		},
		{
			"incorrect format. missing region",
			"aws:///0bdf741206dd9793c",
			"i-0bdf741206dd9793c",
			true,
		},
		{
			"incorrect format. missing aws",
			"///i-0bdf741206dd9793c",
			"i-0bdf741206dd9793c",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instanceID, err := providerIDToInstanceID(tt.providerID)
			if tt.wantErr {
				assert.NotNil(t, err)
				return
			}
			assert.Equal(t, tt.instanceID, instanceID)
			assert.NoError(t, err)
		})
	}
}

func Test_instanceIDToProviderID(t *testing.T) {
	tests := []struct {
		name             string
		instanceID       string
		availabilityZone string
		providerID       string
		wantErr          bool
	}{
		{
			"correct format",
			"i-0bdf741206dd9793c",
			"us-west-2",
			"aws:///us-west-2/i-0bdf741206dd9793c",
			false,
		},
		{
			"wrong format empty instance ID",
			"",
			"us-west-2",
			"aws:///us-west-2/",
			true,
		},
		{
			"wrong format empty availability zone",
			"i-0bdf741206dd9793c",
			"",
			"aws:////i-0bdf741206dd9793c",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerID, err := instanceIDToProviderID(tt.instanceID, tt.availabilityZone)
			if tt.wantErr {
				assert.NotNil(t, err)
				return
			}
			assert.Equal(t, tt.providerID, providerID)
			assert.NoError(t, err)
		})
	}
}

func TestInstance_OutOfDate(t *testing.T) {
	instanceID, anotherID := "i-abcdefghijklmn", "i-anotheridfortest"
	okConfig, notOkConfig, emptyConfig := "ok-config-name", "not-ok-config-name", ""
	configV2, configV3 := "2", "3"

	mockedEC2ServiceLatest := &mockedEC2{
		Resp: ec2.DescribeLaunchTemplateVersionsOutput{
			LaunchTemplateVersions: []*ec2.LaunchTemplateVersion{
				{
					VersionNumber: aws.Int64(3),
				},
			},
		},
	}

	tests := []struct {
		name     string
		group    *autoscaling.Group
		instance *autoscaling.Instance
		expect   bool
	}{
		{
			"test group config instance config ok",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, &okConfig)},
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			buildInstance(&instanceID, &okConfig),
			false,
		},
		{
			"test group config instance config ok with another instance out of date",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, &okConfig), buildInstance(&anotherID, &notOkConfig)},
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			buildInstance(&instanceID, &okConfig),
			false,
		},
		{
			"test group config instance config not ok with another instance ok",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, &notOkConfig), buildInstance(&anotherID, &okConfig)},
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			buildInstance(&instanceID, &notOkConfig),
			true,
		},
		{
			"test group config instance config not ok",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, &notOkConfig)},
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			buildInstance(&instanceID, &notOkConfig),
			true,
		},
		{
			"test group config instance config empty",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, &emptyConfig)},
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			buildInstance(&instanceID, &emptyConfig),
			true,
		},
		{
			"test group config instance config nil",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, nil)},
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			buildInstance(&instanceID, nil),
			true,
		},
		{
			"test group config empty instance config ok",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, &okConfig)},
				LaunchConfigurationName: aws.String(""),
			},
			buildInstance(&instanceID, &okConfig),
			true,
		},
		{
			"test group config nil instance config ok",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, &okConfig)},
				LaunchConfigurationName: nil,
			},
			buildInstance(&instanceID, &okConfig),
			true,
		},
		{
			"test group config nil instance config empty",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, &emptyConfig)},
				LaunchConfigurationName: nil,
			},
			buildInstance(&instanceID, &emptyConfig),
			false,
		},
		{
			"test group config empty instance config empty",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, &emptyConfig)},
				LaunchConfigurationName: aws.String(""),
			},
			buildInstance(&instanceID, &emptyConfig),
			false,
		},
		{
			"test group config nil instance config nil",
			&autoscaling.Group{
				Instances:               []*autoscaling.Instance{buildInstance(&instanceID, nil)},
				LaunchConfigurationName: nil,
			},
			buildInstance(&instanceID, nil),
			false,
		},
		{
			"test group template ok instance template ok",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, &configV3)},
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			buildLTInstance(&instanceID, &configV3),
			false,
		},
		{
			"test group template ok instance template ok with another instance not ok",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, &configV3), buildLTInstance(&anotherID, &configV2)},
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			buildLTInstance(&instanceID, &configV3),
			false,
		},
		{
			"test group template ok instance template not ok with another instance ok",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, &configV2), buildLTInstance(&anotherID, &configV3)},
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			buildLTInstance(&instanceID, &configV2),
			true,
		},
		{
			"test group template ok instance template out of date",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, &configV2)},
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			buildLTInstance(&instanceID, &configV2),
			true,
		},
		{
			"test group template ok instance template nil",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, nil)},
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			buildLTInstance(&instanceID, nil),
			true,
		},
		{
			"test group template nil instance template ok",
			&autoscaling.Group{
				Instances:      []*autoscaling.Instance{buildLTInstance(&instanceID, &configV3)},
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{},
			},
			buildLTInstance(&instanceID, &configV3),
			true,
		},
		{
			"test group template nil instance template nil",
			&autoscaling.Group{
				Instances:      []*autoscaling.Instance{buildLTInstance(&instanceID, nil)},
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{},
			},
			buildLTInstance(&instanceID, nil),
			false,
		},
		{
			"nil everything",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, nil)},
			},
			buildLTInstance(&instanceID, nil),
			false,
		},
		{
			"test group template mixed ok instance template up to date",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, &configV3)},
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{
					LaunchTemplate: &autoscaling.LaunchTemplate{
						LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
							Version: aws.String("3"),
						},
					},
				},
			},
			buildLTInstance(&instanceID, &configV3),
			false,
		},
		{
			"test group template mixed ok instance template out of date",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, &configV2)},
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{
					LaunchTemplate: &autoscaling.LaunchTemplate{
						LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
							Version: aws.String("3"),
						},
					},
				},
			},
			buildLTInstance(&instanceID, &configV2),
			true,
		},
		{
			"test group template mixed nil instance template ok",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, &configV2)},
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{
					LaunchTemplate: &autoscaling.LaunchTemplate{
						LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{},
					},
				},
			},
			buildLTInstance(&instanceID, &configV2),
			true,
		},
		{
			"test group template mixed early nil instance template ok",
			&autoscaling.Group{
				Instances:            []*autoscaling.Instance{buildLTInstance(&instanceID, &configV2)},
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{},
			},
			buildLTInstance(&instanceID, &configV2),
			true,
		},
		{
			"test group template mixed early nil instance template nil",
			&autoscaling.Group{
				Instances:            []*autoscaling.Instance{buildLTInstance(&instanceID, nil)},
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{},
			},
			buildLTInstance(&instanceID, nil),
			false,
		},
		{
			"test group template mixed early ok instance config ok. should not match",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildInstance(&instanceID, &okConfig)},
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{
					LaunchTemplate: &autoscaling.LaunchTemplate{
						LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
							Version: aws.String("3"),
						},
					},
				},
			},
			buildInstance(&instanceID, &okConfig),
			true,
		},
		{
			"test asg set to latest should match if instance is latest",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, &configV3)},
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String(launchTemplateLatestVersion),
				},
			},
			buildLTInstance(&instanceID, &configV3),
			false,
		},
		{
			"test asg set to latest should not match if instance not latest",
			&autoscaling.Group{
				Instances: []*autoscaling.Instance{buildLTInstance(&instanceID, &configV2)},
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String(launchTemplateLatestVersion),
				},
			},
			buildLTInstance(&instanceID, &configV2),
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asg := &autoscalingGroups{
				groups:     []*autoscaling.Group{tt.group},
				ec2Service: mockedEC2ServiceLatest,
				logger:     logr.Discard(),
			}
			outOfDate := asg.instanceOutOfDate(tt.instance)
			assert.Equal(t, tt.expect, outOfDate)
		})
	}
}

// buildInstance creates a new *autoscaling.Instance with Launch Configuration
func buildInstance(instanceID, launchConfigName *string) *autoscaling.Instance {
	return &autoscaling.Instance{
		InstanceId:              instanceID,
		LaunchConfigurationName: launchConfigName,
	}
}

// buildLTInstance creates a new *autoscaling.Instance with Luanch Template
func buildLTInstance(instanceID, configVersion *string) *autoscaling.Instance {
	return &autoscaling.Instance{
		InstanceId: instanceID,
		LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
			Version: configVersion,
		},
	}
}
