package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/labels"
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

	creds := credentials.NewChainCredentials(
		[]credentials.Provider{
			&credentials.SharedCredentialsProvider{},
			&credentials.EnvProvider{},
			&credentials.EC2RoleProvider{},
		})
	// Hardcode region to us-east-1 for now. Perhaps fetch through metadata service
	// curl http://169.254.169.254/latest/meta-data/placement/availability-zone
	awsConfig := aws.Config{
		Credentials: creds,
		Region:      "us-east-1",
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
				continue
			}
			if len(ingress) < 1 {
				glog.Warningf("Multiple ingress points found for ELB, not supported")
				continue
			}
			hn := ingress[0].Hostname

			domain, ok := s.ObjectMeta.Annotations["domainName"]
			if !ok {
				glog.Warningf("Domain name not set for %s", s.Name)
				continue
			}

			glog.Infof("Creating DNS for %s service: %s -> %s", i, s.Name, hn, domain)
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
				continue
			}
			descs := resp.LoadBalancerDescriptions
			if len(descs) < 1 {
				glog.Warningf("No lb found for %s: %v", tld, err)
				continue
			}
			if len(descs) > 1 {
				glog.Warningf("Multiple lbs found for %s: %v", tld, err)
				continue
			}
			hzId := descs[0].CanonicalHostedZoneNameID

			listHostedZoneInput := route53.ListHostedZonesByNameInput{
				DNSName: &tld,
			}
			hzOut, err := r53Api.ListHostedZonesByName(&listHostedZoneInput)
			if err != nil {
				glog.Warningf("No zone found for %s: %v", tld, err)
				continue
			}
			zones := hzOut.HostedZones
			if len(zones) < 1 {
				glog.Warningf("No zone found for %s", tld)
				continue
			}
			// The AWS API may return more than one zone, the first zone should be the relevant one
			tldWithDot := fmt.Sprint(tld, ".")
			if *zones[0].Name != tldWithDot {
				glog.Warningf("Zone found %s does not match tld given %s", *zones[0].Name, tld)
				continue
			}
			zoneId := *zones[0].ID
			zoneParts := strings.Split(zoneId, "/")
			zoneId = zoneParts[len(zoneParts)-1]

			at := route53.AliasTarget{
				DNSName:              &hn,
				EvaluateTargetHealth: aws.Boolean(false),
				HostedZoneID:         hzId,
			}
			rrs := route53.ResourceRecordSet{
				AliasTarget: &at,
				Name:        &domain,
				Type:        aws.String("A"),
			}
			change := route53.Change{
				Action:            aws.String("UPSERT"),
				ResourceRecordSet: &rrs,
			}
			batch := route53.ChangeBatch{
				Changes: []*route53.Change{&change},
				Comment: aws.String("Kubernetes Update to Service"),
			}
			crrsInput := route53.ChangeResourceRecordSetsInput{
				ChangeBatch:  &batch,
				HostedZoneID: &zoneId,
			}
			_, err = r53Api.ChangeResourceRecordSets(&crrsInput)
			if err != nil {
				glog.Warningf("Failed to update record set: %v", err)
				continue
			}
			glog.Infof("Created dns record set: tld=%s, subdomain=%s, zoneId=%s", tld, subdomain, zoneId)
		}
		time.Sleep(30 * time.Second)
	}
}
