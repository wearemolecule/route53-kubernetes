package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"io/ioutil"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/golang/glog"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/route53"
)

func main() {
	flag.Parse()
	glog.Info("Route53 Update Service")
	kubernetesService := os.Getenv("KUBERNETES_SERVICE_HOST")
	if kubernetesService == "" {
		glog.Fatalf("Please specify the Kubernetes server with --server")
	}
	apiServer := fmt.Sprintf("https://%s:%s", kubernetesService, os.Getenv("KUBERNETES_SERVICE_PORT"))

	token, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		glog.Fatalf("No service account token found")
	}

	config := client.Config{
		Host:        apiServer,
		BearerToken: string(token),
		Insecure:    true,
	}

	c, err := client.New(&config)
	if err != nil {
		glog.Fatalf("Failed to make client: %v", err)
	}

	creds := credentials.NewCredentials(&credentials.EC2RoleProvider{})
	awsConfig := aws.Config{
		Credentials: creds,
	}
	r53Api := route53.New(&awsConfig)

	selector := "dns=route53"
	l, err := labels.Parse(selector)
	if err != nil {
		glog.Fatalf("Failed to parse selector %q: %v", selector, err)
	}

	glog.Infof("Starting Service Polling every 30s")
	for {
		services, err := c.Services(api.NamespaceAll).List(l)
		if err != nil {
			glog.Fatalf("Failed to list pods: %v", err)
		}

		glog.Infof("Found %d DNS services in all namespaces with selector %q", len(services.Items), selector)
		for i := range services.Items {
			s := &services.Items[i]
			ingress := s.Status.LoadBalancer.Ingress
			if len(ingress) < 1 {
				glog.Warningf("No ingress defined for ELB")
				break
			}
			if len(ingress) < 1 {
				glog.Warningf("Multiple ingress points found for ELB, not supported")
				break
			}
			hn := ingress[0].Hostname

			domain, ok := s.ObjectMeta.Annotations["domainName"]
			if !ok {
				glog.Warningf("Domain name not set for %s", s.Name)
				break
			}

			glog.Infof("%d: %s Service: %s -> %s", i, s.Name, hn, domain)
			domainParts := strings.Split(domain, ".")
			segments := len(domainParts)
			tld := strings.Join(domainParts[segments-2:], ".")
			subdomain := strings.Join(domainParts[:segments-2], ".")

			listHostedZoneInput := route53.ListHostedZonesByNameInput{
				DNSName: &tld,
			}
			hzOut, err := r53Api.ListHostedZonesByName(&listHostedZoneInput)
			if err != nil {
				glog.Warningf("No zone found for %s: %v", tld, err)
				break
			}
			zones := hzOut.HostedZones
			if len(zones) < 1 {
				glog.Warningf("No zone found for %s: %v", tld, err)
				break
			}
			if len(zones) > 1 {
				glog.Warningf("Multiple zones found for %s: %v", tld, err)
				break
			}
			zoneId := zones[0].ID
			glog.Infof("Found these things: tld=%s, subdomain=%s, zoneId=%s", tld, subdomain, *zoneId)
		}
		time.Sleep(30 * time.Second)
	}
}
