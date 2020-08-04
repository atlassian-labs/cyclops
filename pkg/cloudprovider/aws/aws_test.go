package aws

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
)

func TestInstance_OutOfDate(t *testing.T) {
	tests := []struct {
		name     string
		group    *autoscaling.Group
		instance *autoscaling.Instance
		expect   bool
	}{
		{
			"test group config instance config ok",
			&autoscaling.Group{
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			&autoscaling.Instance{
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			false,
		},
		{
			"test group config instance config not ok",
			&autoscaling.Group{
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			&autoscaling.Instance{
				LaunchConfigurationName: aws.String("not-ok-config-name"),
			},
			true,
		},
		{
			"test group config instance config empty",
			&autoscaling.Group{
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			&autoscaling.Instance{
				LaunchConfigurationName: aws.String(""),
			},
			true,
		},
		{
			"test group config instance config nil",
			&autoscaling.Group{
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			&autoscaling.Instance{
				LaunchConfigurationName: nil,
			},
			true,
		},
		{
			"test group config empty instance config ok",
			&autoscaling.Group{
				LaunchConfigurationName: aws.String(""),
			},
			&autoscaling.Instance{
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			true,
		},
		{
			"test group config nil instance config ok",
			&autoscaling.Group{
				LaunchConfigurationName: nil,
			},
			&autoscaling.Instance{
				LaunchConfigurationName: aws.String("ok-config-name"),
			},
			true,
		},
		{
			"test group config nil instance config empty",
			&autoscaling.Group{
				LaunchConfigurationName: nil,
			},
			&autoscaling.Instance{
				LaunchConfigurationName: aws.String(""),
			},
			false,
		},
		{
			"test group config empty instance config empty",
			&autoscaling.Group{
				LaunchConfigurationName: aws.String(""),
			},
			&autoscaling.Instance{
				LaunchConfigurationName: aws.String(""),
			},
			false,
		},
		{
			"test group config nil instance config nil",
			&autoscaling.Group{
				LaunchConfigurationName: nil,
			},
			&autoscaling.Instance{
				LaunchConfigurationName: nil,
			},
			false,
		},
		{
			"test group template ok instance template ok",
			&autoscaling.Group{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			false,
		},
		{
			"test group template ok instance template out of date",
			&autoscaling.Group{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("2"),
				},
			},
			true,
		},
		{
			"test group template ok instance template nil",
			&autoscaling.Group{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{},
			},
			true,
		},
		{
			"test group template nil instance template ok",
			&autoscaling.Group{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			true,
		},
		{
			"test group template nil instance template nil",
			&autoscaling.Group{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{},
			},
			false,
		},
		{
			"nil everything",
			&autoscaling.Group{},
			&autoscaling.Instance{},
			false,
		},
		{
			"test group template mixed ok instance template up to date",
			&autoscaling.Group{
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{
					LaunchTemplate: &autoscaling.LaunchTemplate{
						LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
							Version: aws.String("3"),
						},
					},
				},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("3"),
				},
			},
			false,
		},
		{
			"test group template mixed ok instance template out of date",
			&autoscaling.Group{
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{
					LaunchTemplate: &autoscaling.LaunchTemplate{
						LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
							Version: aws.String("3"),
						},
					},
				},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("2"),
				},
			},
			true,
		},
		{
			"test group template mixed nil instance template ok",
			&autoscaling.Group{
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{
					LaunchTemplate: &autoscaling.LaunchTemplate{
						LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{},
					},
				},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("2"),
				},
			},
			true,
		},
		{
			"test group template mixed early nil instance template ok",
			&autoscaling.Group{
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{
					Version: aws.String("2"),
				},
			},
			true,
		},
		{
			"test group template mixed early nil instance template nil",
			&autoscaling.Group{
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{},
			},
			&autoscaling.Instance{
				LaunchTemplate: &autoscaling.LaunchTemplateSpecification{},
			},
			false,
		},
		{
			"test group template mixed early ok instance config ok. should not match",
			&autoscaling.Group{
				MixedInstancesPolicy: &autoscaling.MixedInstancesPolicy{
					LaunchTemplate: &autoscaling.LaunchTemplate{
						LaunchTemplateSpecification: &autoscaling.LaunchTemplateSpecification{
							Version: aws.String("3"),
						},
					},
				},
			},
			&autoscaling.Instance{
				LaunchConfigurationName: aws.String("launch-config-uuid"),
			},
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asg := &autoscalingGroup{
				group: tt.group,
			}
			outOfDate := asg.instanceOutOfDate(tt.instance)
			assert.Equal(t, tt.expect, outOfDate)
		})
	}
}
