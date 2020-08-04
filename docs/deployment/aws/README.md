# AWS

- [AWS](#aws)
  - [Permissions](#permissions)
  - [AWS Credentials](#aws-credentials)
  - [Common issues, caveats and gotchas](#common-issues-caveats-and-gotchas)


Current the only cloud provider Cyclops supports is AWS, so it will be enabled automatically

## Permissions

Cyclops requires the following IAM policy to be able to properly integrate with AWS:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "autoscaling:DescribeAutoScalingGroups",
        "autoscaling:DetachInstances",
        "ec2:TerminateInstances",
        "ec2:DescribeInstances"
      ],
      "Resource": "*"
    }
  ]
}
```

## AWS Credentials

Cyclops makes use of [aws-sdk-go](https://github.com/aws/aws-sdk-go) for communicating with the AWS API to perform
scaling of auto scaling groups and terminating of instances.

To initiate the session, Cyclops uses the [session](https://docs.aws.amazon.com/sdk-for-go/api/aws/session/) package:

```go
sess, err := session.NewSession()
```

This will use the [default credential chain](https://docs.aws.amazon.com/sdk-for-go/api/aws/defaults/#CredChain)
for obtaining access.

See [Configuring the AWS SDK for Go](https://docs.aws.amazon.com/sdk-for-go/v1/developer-guide/configuring-sdk.html)
for more information on how the SDK obtains access.

It is highly recommended to use IAM roles for Cyclops access, using the above IAM policy.

## Common issues, caveats and gotchas

- Ensure that if you are using the remote credential provider that `AWS_REGION` or `AWS_DEFAULT_REGION` is set to the region that the auto scaling groups reside in.
- Cyclops does not support cycling auto scaling groups that are located in different regions, they must all reside in the same region.
