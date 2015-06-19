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
	"github.com/aws/aws-sdk-go/service/elb"
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
	// Hardcode region to us-east-1 for now. Perhaps fetch through metadata service
	awsConfig := aws.Config{
		Credentials: creds,
		Region: "us-east-1",
	}
	r53Api := route53.New(&awsConfig)
	elbApi := elb.New(&awsConfig)

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

			elbName := strings.Split(hn, "-")[0]
			lbInput := &elb.DescribeLoadBalancersInput{
				LoadBalancerNames: []*string{
					&elbName,
				},
			}
			resp, err := elbApi.DescribeLoadBalancers(lbInput)
			if err != nil {
				glog.Warningf("Could not describe load balancer: %v", err)
				break
			}
			descs := resp.LoadBalancerDescriptions
			if len(descs) < 1 {
				glog.Warningf("No lb found for %s: %v", tld, err)
				break
			}
			if len(descs) > 1 {
				glog.Warningf("Multiple lbs found for %s: %v", tld, err)
				break
			}
			hzId := descs[0].CanonicalHostedZoneNameID

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

			var ttl int64 = 3600
			at := route53.AliasTarget{
				DNSName: &hn,
				HostedZoneID: hzId,
			}
			rrs := route53.ResourceRecordSet{
				AliasTarget: &at,
				Name: &subdomain,
				TTL: &ttl,
			}
			change := route53.Change{
				Action: aws.String("UPSERT"),
				ResourceRecordSet: &rrs,
			}
			batch := route53.ChangeBatch{
				Changes: []*route53.Change{&change},
				Comment: aws.String("Kubernetes Update to Service"),
			}
			crrsInput := route53.ChangeResourceRecordSetsInput{
				ChangeBatch: &batch,
				HostedZoneID: zoneId,
			}
			_, err = r53Api.ChangeResourceRecordSets(&crrsInput)
			if err != nil {
				glog.Warningf("Failed to update record set")
				break
			}
		}
		time.Sleep(30 * time.Second)
	}
}
