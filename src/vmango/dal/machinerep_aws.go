package dal

import (
	"encoding/base64"
	"fmt"
	"vmango/models"

	"github.com/Sirupsen/logrus"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	aws_ec2 "github.com/aws/aws-sdk-go/service/ec2"
)

type AWSMachinerep struct {
	ec2 *aws_ec2.EC2

	name       string
	subnetId   string
	secGroups  []string
	assignTags map[string]string
	planmap    map[string]string
}

func NewAWSMachinerep(ec2 *aws_ec2.EC2, name, subnetId string, secGroups []string, assignTags, planmap map[string]string) *AWSMachinerep {
	return &AWSMachinerep{
		ec2:        ec2,
		name:       name,
		subnetId:   subnetId,
		secGroups:  secGroups,
		assignTags: assignTags,
		planmap:    planmap,
	}
}

func (repo *AWSMachinerep) fillVm(vm *models.VirtualMachine, instance *aws_ec2.Instance) error {
	vm.Id = *instance.InstanceId
	for _, tag := range instance.Tags {
		if *tag.Key == "Name" {
			vm.Name = *tag.Value
		}
		if *tag.Key == "vmango:creator" {
			vm.Creator = *tag.Value
		}
		if *tag.Key == "vmango:os" {
			vm.OS = *tag.Value
		}
	}

	vm.Arch = models.ParseHWArch(*instance.Architecture)
	vm.ImageId = *instance.ImageId
	vm.Ip = &models.IP{Address: *instance.PrivateIpAddress}
	vm.HWAddr = *instance.NetworkInterfaces[0].MacAddress

	awsInstanceType := AWS_INSTANCE_TYPES[*instance.InstanceType]
	if awsInstanceType == nil {
		logrus.WithField("instance_type", *instance.InstanceType).Warning("unknown aws instance type")
	} else {
		vm.Cpus = awsInstanceType.Cpus
		vm.Memory = awsInstanceType.Memory
	}

	switch *instance.State.Name {
	case aws_ec2.InstanceStateNameRunning:
		vm.State = models.STATE_RUNNING
	case aws_ec2.InstanceStateNameStopped:
		vm.State = models.STATE_STOPPED
	case aws_ec2.InstanceStateNameStopping, aws_ec2.InstanceStateNameShuttingDown:
		vm.State = models.STATE_STOPPING
	case aws_ec2.InstanceStateNamePending:
		vm.State = models.STATE_STARTING
	default:
		vm.State = models.STATE_UNKNOWN
	}

	attributeResponse, err := repo.ec2.DescribeInstanceAttribute(&aws_ec2.DescribeInstanceAttributeInput{
		Attribute:  aws.String(aws_ec2.InstanceAttributeNameUserData),
		InstanceId: aws.String(vm.Id),
	})
	if err != nil {
		return fmt.Errorf("failed to fetch userdata for instance '%s': %s", vm.Id, err)
	}
	if attributeResponse.UserData.Value != nil {
		userdata, err := base64.StdEncoding.DecodeString(*attributeResponse.UserData.Value)
		if err != nil {
			logrus.WithError(err).WithField("instance_id", vm.Id).Warning("failed to decode userdata")
		} else {
			vm.Userdata = string(userdata)
		}
	}
	for _, blockdev := range instance.BlockDeviceMappings {
		if *blockdev.DeviceName == *instance.RootDeviceName {
			volumeInfo, err := repo.ec2.DescribeVolumes(&aws_ec2.DescribeVolumesInput{
				VolumeIds: []*string{blockdev.Ebs.VolumeId},
			})
			if err != nil {
				return fmt.Errorf("failed to describe root volume: %s", err)
			}
			vm.RootDisk = &models.VirtualMachineDisk{
				Size:   uint64(*volumeInfo.Volumes[0].Size * 1024 * 1024 * 1024),
				Type:   "EBS",
				Driver: "EBS",
			}
		}
	}
	if vm.RootDisk == nil {
		return fmt.Errorf("root drive not found for machine %s", vm.Id)
	}

	vm.SSHKeys = models.SSHKeyList{
		&models.SSHKey{Name: *instance.KeyName},
	}

	if vm.Name == "" {
		vm.Name = vm.Id
	}

	return nil
}

func (repo *AWSMachinerep) List(vms *models.VirtualMachineList) error {
	awsResponse, err := repo.ec2.DescribeInstances(&aws_ec2.DescribeInstancesInput{
		Filters: []*aws_ec2.Filter{
			&aws_ec2.Filter{
				Name:   aws.String("subnet-id"),
				Values: aws.StringSlice([]string{repo.subnetId}),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("cannot describe aws instances: %s", err)
	}
	for _, reservation := range awsResponse.Reservations {
		for _, instance := range reservation.Instances {
			vm := &models.VirtualMachine{}
			if err := repo.fillVm(vm, instance); err != nil {
				return err
			}
			*vms = append(*vms, vm)
		}
	}
	return nil
}

func (repo *AWSMachinerep) Get(vm *models.VirtualMachine) (bool, error) {
	instances, err := repo.ec2.DescribeInstances(&aws_ec2.DescribeInstancesInput{
		InstanceIds: aws.StringSlice([]string{vm.Id}),
	})
	if err != nil {
		return false, fmt.Errorf("failed to describe instance '%s'", vm.Id)
	}
	if err := repo.fillVm(vm, instances.Reservations[0].Instances[0]); err != nil {
		return true, fmt.Errorf("failed to fetch info about vm '%s': %s", vm.Id, err)
	}
	return true, nil
}

func (repo *AWSMachinerep) getKeyPair(sshkey *models.SSHKey) (string, error) {
	keyResponse, err := repo.ec2.DescribeKeyPairs(&aws_ec2.DescribeKeyPairsInput{
		KeyNames: []*string{aws.String(sshkey.Name)},
	})
	if err == nil {
		return *keyResponse.KeyPairs[0].KeyName, nil
	}
	castedErr, ok := err.(awserr.Error)
	if !ok {
		return "", err
	}
	if castedErr.Code() != "InvalidKeyPair.NotFound" {
		return "", err
	}
	importResponse, err := repo.ec2.ImportKeyPair(&aws_ec2.ImportKeyPairInput{
		KeyName:           aws.String(sshkey.Name),
		PublicKeyMaterial: []byte(sshkey.Public),
	})
	if err != nil {
		return "", err
	}
	return *importResponse.KeyName, nil

}

func (repo *AWSMachinerep) Create(vm *models.VirtualMachine, image *models.Image, plan *models.Plan) error {
	if len(vm.SSHKeys) > 1 {
		return fmt.Errorf("Only one ssh key allowed for aws provider")
	}
	instanceTags := []*aws_ec2.Tag{}
	for tagName, tagValue := range repo.assignTags {
		instanceTags = append(instanceTags, &aws_ec2.Tag{
			Key:   aws.String(tagName),
			Value: aws.String(tagValue),
		})
	}
	instanceTags = append(instanceTags, &aws_ec2.Tag{
		Key:   aws.String("Name"),
		Value: aws.String(vm.Name),
	})
	instanceTags = append(instanceTags, &aws_ec2.Tag{
		Key:   aws.String("vmango:creator"),
		Value: aws.String(vm.Creator),
	})
	instanceTags = append(instanceTags, &aws_ec2.Tag{
		Key:   aws.String("vmango:os"),
		Value: aws.String(vm.OS),
	})

	instanceType := repo.planmap[plan.Name]
	if instanceType == "" {
		return fmt.Errorf("plan '%s' not mapped", plan.Name)
	}

	imagesResponse, err := repo.ec2.DescribeImages(&aws_ec2.DescribeImagesInput{
		ImageIds: []*string{aws.String(image.Id)},
	})
	if err != nil {
		return fmt.Errorf("failed to describe image '%s': %s", image.Id, err)
	}

	keyPair, err := repo.getKeyPair(vm.SSHKeys[0])
	if err != nil {
		return fmt.Errorf("failed to fetch or create keypair for selected ssh keys: %s", err)
	}

	params := &aws_ec2.RunInstancesInput{
		ImageId:          aws.String(image.Id),
		InstanceType:     aws.String(instanceType),
		KeyName:          aws.String(keyPair),
		SecurityGroupIds: aws.StringSlice(repo.secGroups),
		SubnetId:         aws.String(repo.subnetId),
		MaxCount:         aws.Int64(1),
		MinCount:         aws.Int64(1),
		UserData:         aws.String(base64.StdEncoding.EncodeToString([]byte(vm.Userdata))),
		TagSpecifications: []*aws_ec2.TagSpecification{
			&aws_ec2.TagSpecification{
				ResourceType: aws.String("instance"),
				Tags:         instanceTags,
			},
		},
		BlockDeviceMappings: []*aws_ec2.BlockDeviceMapping{
			&aws_ec2.BlockDeviceMapping{
				DeviceName: imagesResponse.Images[0].RootDeviceName,
				Ebs: &aws_ec2.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					VolumeSize:          aws.Int64(int64(plan.DiskSizeGigabytes())),
					VolumeType:          aws.String("gp2"),
				},
			},
		},
	}
	runResponse, err := repo.ec2.RunInstances(params)
	if err != nil {
		return fmt.Errorf("failed to run aws instance: %s", err)
	}
	awsInstanceId := runResponse.Instances[0].InstanceId

	waitParams := &aws_ec2.DescribeInstancesInput{
		InstanceIds: []*string{awsInstanceId},
	}
	if err := repo.ec2.WaitUntilInstanceRunning(waitParams); err != nil {
		return fmt.Errorf("failed to wait for new instance '%s': %s", *awsInstanceId, err)
	}

	describeResponse, err := repo.ec2.DescribeInstances(&aws_ec2.DescribeInstancesInput{
		InstanceIds: []*string{awsInstanceId},
	})
	if err != nil {
		return fmt.Errorf("failed to describe new instance '%s': %s", *awsInstanceId, err)
	}
	if err := repo.fillVm(vm, describeResponse.Reservations[0].Instances[0]); err != nil {
		return fmt.Errorf("failed to fetch info about new instance '%s': %s", *awsInstanceId, err)
	}
	return nil
}

func (repo *AWSMachinerep) Start(vm *models.VirtualMachine) error {
	_, err := repo.ec2.StartInstances(&aws_ec2.StartInstancesInput{
		InstanceIds: []*string{aws.String(vm.Id)},
	})
	return err
}

func (repo *AWSMachinerep) Stop(vm *models.VirtualMachine) error {
	_, err := repo.ec2.StopInstances(&aws_ec2.StopInstancesInput{
		Force:       aws.Bool(true),
		InstanceIds: []*string{aws.String(vm.Id)},
	})
	return err
}

func (repo *AWSMachinerep) Remove(vm *models.VirtualMachine) error {
	_, err := repo.ec2.TerminateInstances(&aws_ec2.TerminateInstancesInput{
		InstanceIds: []*string{aws.String(vm.Id)},
	})
	return err
}

func (repo *AWSMachinerep) Reboot(vm *models.VirtualMachine) error {
	_, err := repo.ec2.RebootInstances(&aws_ec2.RebootInstancesInput{
		InstanceIds: []*string{aws.String(vm.Id)},
	})
	return err
}

func (repo *AWSMachinerep) ServerInfo(serverInfoList *models.ServerList) error {
	serverInfo := &models.Server{}
	serverInfo.Type = "aws"
	serverInfo.Data = map[string]interface{}{}

	serverInfo.Data["Hostname"] = fmt.Sprintf("EC2 %s", repo.ec2.ClientInfo.APIVersion)

	serverInfo.Data["Cpus"] = "unlimited"
	serverInfo.Data["Model"] = "unknown"
	serverInfo.Data["Memory"] = "unlimited"
	serverInfo.Data["Processor"] = "unknown"

	serverInfo.Data["StorageUsagePercent"] = 1
	serverInfo.Data["LibvirtURI"] = repo.ec2.ClientInfo.Endpoint
	memUsedPercent := 1
	serverInfo.Data["MemoryUsedPersent"] = memUsedPercent

	resp, err := repo.ec2.DescribeInstances(&aws_ec2.DescribeInstancesInput{})
	if err != nil {
		return fmt.Errorf("cannot describe instances: %s", err)
	}
	machineCount := 0
	for _, reservation := range resp.Reservations {
		machineCount += len(reservation.Instances)
	}
	serverInfo.Data["MachineCount"] = machineCount

	*serverInfoList = append(*serverInfoList, serverInfo)
	return nil
}
