package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/ec2rolecreds"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/route53"
	"github.com/golang/glog"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/client/restclient"
	"k8s.io/kubernetes/pkg/client/transport"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/labels"
)

func main() {
	flag.Parse()
	glog.Info("Route53 Update Service")
	kubernetesService := os.Getenv("KUBERNETES_SERVICE_HOST")
	kubernetesServicePort := os.Getenv("KUBERNETES_SERVICE_PORT")
	if kubernetesService == "" {
		glog.Fatal("Please specify the Kubernetes server via KUBERNETES_SERVICE_HOST")
	}
	if kubernetesServicePort == "" {
		kubernetesServicePort = "443"
	}
	apiServer := fmt.Sprintf("https://%s:%s", kubernetesService, kubernetesServicePort)

	caFilePath := os.Getenv("CA_FILE_PATH")
	certFilePath := os.Getenv("CERT_FILE_PATH")
	keyFilePath := os.Getenv("KEY_FILE_PATH")
	if caFilePath == "" || certFilePath == "" || keyFilePath == "" {
		glog.Fatal("You must provide paths for CA, Cert, and Key files")
	}

	tls := transport.TLSConfig{
		CAFile:   caFilePath,
		CertFile: certFilePath,
		KeyFile:  keyFilePath,
	}
	// tlsTransport := transport.New(transport.Config{TLS: tls})
	tlsTransport, err := transport.New(&transport.Config{TLS: tls})
	if err != nil {
		glog.Fatalf("Couldn't set up tls transport: %s", err)
	}

	config := restclient.Config{
		Host:      apiServer,
		Transport: tlsTransport,
	}

	c, err := client.New(&config)
	if err != nil {
		glog.Fatalf("Failed to make client: %v", err)
	}
	glog.Infof("Connected to kubernetes @ %s", apiServer)

	metadata := ec2metadata.New(session.New())

	creds := credentials.NewChainCredentials(
		[]credentials.Provider{
			&credentials.EnvProvider{},
			&credentials.SharedCredentialsProvider{},
			&ec2rolecreds.EC2RoleProvider{Client: metadata},
		})

	region, err := metadata.Region()
	if err != nil {
		glog.Fatalf("Unable to retrieve the region from the EC2 instance %v\n", err)
	}

	awsConfig := aws.NewConfig()
	awsConfig.WithCredentials(creds)
	awsConfig.WithRegion(region)
	sess := session.New(awsConfig)

	r53Api := route53.New(sess)
	elbApi := elb.New(sess)
	if r53Api == nil || elbApi == nil {
		glog.Fatal("Failed to make AWS connection")
	}

	selector := "dns=route53"
	l, err := labels.Parse(selector)
	if err != nil {
		glog.Fatalf("Failed to parse selector %q: %v", selector, err)
	}
	listOptions := api.ListOptions{
		LabelSelector: l,
	}

	glog.Infof("Starting Service Polling every 30s")
	for {
		services, err := c.Services(api.NamespaceAll).List(listOptions)
		if err != nil {
			glog.Fatalf("Failed to list pods: %v", err)
		}

		glog.Infof("Found %d DNS services in all namespaces with selector %q", len(services.Items), selector)
		for i := range services.Items {
			s := &services.Items[i]
			hn, err := serviceHostname(s)
			if err != nil {
				glog.Warningf("Couldn't find hostname: %s", err)
				continue
			}

			domain, ok := s.ObjectMeta.Annotations["domainName"]
			if !ok {
				glog.Warningf("Domain name not set for %s", s.Name)
				continue
			}

			glog.Infof("Creating DNS for %s service: %s -> %s", s.Name, hn, domain)
			domainParts := strings.Split(domain, ".")
			segments := len(domainParts)
			if segments < 3 {
				glog.Warningf("Domain %s is invalid - it should be a fully qualified domain name and subdomain (i.e. test.example.com)", domain)
				continue
			}
			tld := strings.Join(domainParts[segments-2:], ".")
			subdomain := strings.Join(domainParts[:segments-2], ".")

			hzId, err := hostedZoneId(elbApi, hn)
			if err != nil {
				glog.Warningf("Couldn't get zone ID: %s", err)
				continue
			}

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
			zoneId := *zones[0].Id
			zoneParts := strings.Split(zoneId, "/")
			zoneId = zoneParts[len(zoneParts)-1]

			if err = updateDns(r53Api, hn, hzId, domain, zoneId); err != nil {
				glog.Warning(err)
				continue
			}
			glog.Infof("Created dns record set: tld=%s, subdomain=%s, zoneId=%s", tld, subdomain, zoneId)
		}
		time.Sleep(30 * time.Second)
	}
}

func serviceHostname(service *api.Service) (string, error) {
	ingress := service.Status.LoadBalancer.Ingress
	if len(ingress) < 1 {
		return "", errors.New("No ingress defined for ELB")
	}
	if len(ingress) > 1 {
		return "", errors.New("Multiple ingress points found for ELB, not supported")
	}
	return ingress[0].Hostname, nil
}

func hostedZoneId(elbApi *elb.ELB, hostname string) (string, error) {
	hostnameSegments := strings.Split(hostname, "-")
	elbName := hostnameSegments[0]
	
	// handle internal load balancer naming
	if elbName == "internal" {
		elbName = hostnameSegments[1]
	}
	
	lbInput := &elb.DescribeLoadBalancersInput{
		LoadBalancerNames: []*string{
			&elbName,
		},
	}
	resp, err := elbApi.DescribeLoadBalancers(lbInput)
	if err != nil {
		return "", fmt.Errorf("Could not describe load balancer: %v", err)
	}
	descs := resp.LoadBalancerDescriptions
	if len(descs) < 1 {
		return "", fmt.Errorf("No lb found: %v", err)
	}
	if len(descs) > 1 {
		return "", fmt.Errorf("Multiple lbs found: %v", err)
	}
	return *descs[0].CanonicalHostedZoneNameID, nil
}

func updateDns(r53Api *route53.Route53, hn, hzId, domain, zoneId string) error {
	at := route53.AliasTarget{
		DNSName:              &hn,
		EvaluateTargetHealth: aws.Bool(false),
		HostedZoneId:         &hzId,
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
		HostedZoneId: &zoneId,
	}
	_, err := r53Api.ChangeResourceRecordSets(&crrsInput)
	if err != nil {
		return fmt.Errorf("Failed to update record set: %v", err)
	}
	return nil
}
