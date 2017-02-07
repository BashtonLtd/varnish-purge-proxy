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
	"context"
	"log"
	"os"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	compute "google.golang.org/api/compute/v1"
)

// GCEProvider struct
type GCEProvider struct {
	Service     *compute.Service
	Credentials string
	Debug       bool
	NamePrefix  string
	Project     string
	Region      string
}

// Auth takes config values and configures this service
func (g *GCEProvider) Auth() error {
	os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", g.Credentials)
	src, err := google.DefaultTokenSource(oauth2.NoContext, compute.ComputeReadonlyScope)
	if err != nil {
		log.Fatalf("Unable to acquire token source: %v", err)
		return err
	}

	oauthClient := oauth2.NewClient(context.Background(), src)

	svc, err := compute.New(oauthClient)
	if err != nil {
		log.Fatal("Unable to get client: ", err)
		return err
	}
	g.Service = svc
	return nil
}

// GetPrivateIPs returns the IPs of instances matching specific tags
func (g *GCEProvider) GetPrivateIPs() []string {
	instances := []string{}
	ctx := context.Background()

	var zones []string
	{
		call := g.Service.Zones.List(g.Project)
		call.Filter("region eq .*" + g.Region + ".*")
		if err := call.Pages(ctx, func(page *compute.ZoneList) error {
			for _, v := range page.Items {
				log.Printf("Found zone: %s", v.Name)
				zones = append(zones, v.Name)
			}
			return nil
		}); err != nil {
			log.Fatal("Failed to list zones: ", err)
		}
	}

	for _, zone := range zones {
		log.Printf("Checking zone: %s", zone)
		call := g.Service.Instances.List(g.Project, zone)
		call.Filter("(name eq .*" + g.NamePrefix + ".*) (status eq RUNNING)")
		if err := call.Pages(ctx, func(page *compute.InstanceList) error {
			for _, v := range page.Items {
				log.Printf("Found instance: %s", v.Name)
				for _, n := range v.NetworkInterfaces {
					log.Printf("Found address: %s", n.NetworkIP)
					instances = append(instances, n.NetworkIP)
				}
			}
			return nil
		}); err != nil {
			log.Fatal("Failed to list instances: ", err)
		}
	}

	return instances
}
