package providers

/*
 * varnish-purge-proxy
 * (C) Copyright Bashton Ltd, 2014
 *
 * varnish-purge-proxy is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * varnish-purge-proxy is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with varnish-purge-proxy.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

import (
	"fmt"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// AWSProvider struct
type AWSProvider struct {
	Service *ec2.EC2
	Tags    []string
	Debug   bool
}

// Auth takes config values and configures this service
func (a *AWSProvider) Auth() error {
	region, err := ec2metadata.New(session.New()).Region()
	if err != nil {
		log.Printf("Unable to retrieve the region from the EC2 instance %v\n", err)
		return err
	}

	// Set up access to ec2
	svc := ec2.New(session.New(), &aws.Config{Region: &region})

	a.Service = svc
	return nil
}

// GetPrivateIPs returns the IPs of instances matching specific tags
func (a *AWSProvider) GetPrivateIPs() []string {
	instances := []string{}
	filters, err := a.buildFilter()
	if err != nil {
		log.Println(err)
	}

	request := ec2.DescribeInstancesInput{Filters: filters}
	result, err := a.Service.DescribeInstances(&request)
	if err != nil {
		log.Println(err)
	}

	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if instance.PrivateIpAddress != nil {
				if a.Debug {
					log.Printf("Adding %s to IP list\n", *instance.PrivateIpAddress)
				}
				instances = append(instances, *instance.PrivateIpAddress)
			}
		}
	}

	return instances
}

func (a *AWSProvider) buildFilter() ([]*ec2.Filter, error) {
	filters := []*ec2.Filter{}

	for _, tag := range a.Tags {
		parts := strings.SplitN(tag, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("expected TAG:VALUE got %s", tag)
		}
		tagName := fmt.Sprintf("tag:%s", *aws.String(parts[0]))
		filters = append(filters, &ec2.Filter{
			Name:   &tagName,
			Values: []*string{aws.String(parts[1])},
		})
	}
	return filters, nil

}
