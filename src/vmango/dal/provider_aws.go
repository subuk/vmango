package dal

import (
	"fmt"
	"vmango/cfg"
	"vmango/models"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	aws_session "github.com/aws/aws-sdk-go/aws/session"
	aws_ec2 "github.com/aws/aws-sdk-go/service/ec2"
)

const AWS_PROVIDER_TYPE = "AWS"

type AWSProvider struct {
	name     string
	machines Machinerep
	images   Imagerep
	ec2      *aws_ec2.EC2
	region   string
	profile  string
}

func NewAWSProvider(conf cfg.AWSConnectionConfig) *AWSProvider {
	awsSession := aws_session.Must(aws_session.NewSessionWithOptions(aws_session.Options{
		Profile: conf.Profile,
		Config:  aws.Config{Region: aws.String(conf.Region)},
	}))

	if conf.AccessKey != "" && conf.SecretKey != "" {
		awsSession.Config = awsSession.Config.WithCredentials(
			credentials.NewStaticCredentials(conf.AccessKey, conf.SecretKey, ""),
		)
	}

	ec2 := aws_ec2.New(awsSession)
	imagerep := NewAWSImagerep(ec2, conf.Images)
	machinerep := NewAWSMachinerep(
		ec2,
		conf.Name,
		conf.SubnetId,
		conf.SecurityGroups,
		conf.AssignTags,
		conf.PlanMap,
	)
	return &AWSProvider{
		name:     conf.Name,
		machines: machinerep,
		images:   imagerep,
		ec2:      ec2,
		region:   conf.Region,
		profile:  conf.Profile,
	}

}

func (p *AWSProvider) Name() string {
	return p.name
}

func (p *AWSProvider) Images() Imagerep {
	return p.images
}

func (p *AWSProvider) Machines() Machinerep {
	return p.machines
}

func (p *AWSProvider) Status(status *models.StatusInfo) error {
	// Basic info
	status.Name = p.Name()
	status.Type = AWS_PROVIDER_TYPE

	// Description
	status.Description = fmt.Sprintf("AWS region %s", p.region)

	// Connection
	status.Connection = fmt.Sprintf("Profile %s", p.profile)

	// Storage info

	// Memory info

	// Machine count
	machines := models.VirtualMachineList{}
	if err := p.Machines().List(&machines); err != nil {
		return fmt.Errorf("failed to count machines: %s", err)
	}
	status.MachineCount = machines.Count()
	return nil
}
