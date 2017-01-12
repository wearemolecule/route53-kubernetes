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
	"strconv"
)

// Don't actually commit the changes to route53 records, just print out what we would have done.
var dryRun bool

const (
	A  = "A"
	CNAME = "CNAME"
)

type rule struct {
	service       	*api.Service
	dnsRecordType 	string
	ttl 		int64
}

func init() {
	dryRunStr := os.Getenv("DRY_RUN")
	if dryRunStr != "" {
		dryRun = true
	}
}

func main() {
	flag.Parse()
	glog.Info("Route53 Update Service")

	config, err := restclient.InClusterConfig()
	if err != nil {
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
			glog.Fatalf("Couldn't set up TLS transport: %s", err)
		}

		config = &restclient.Config{
			Host:      apiServer,
			Transport: tlsTransport,
		}
	}

	c, err := client.New(config)
	if err != nil {
		glog.Fatalf("Failed to create new client: %v", err)
	}
	glog.Infof("Connected to kubernetes @ %s", config.Host)

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
	elbAPI := elb.New(sess)
	if r53Api == nil || elbAPI == nil {
		glog.Fatal("Failed to establish AWS connection")
	}

	selector := "dns=route53"
	l, err := labels.Parse(selector)
	if err != nil {
		glog.Fatalf("Failed to parse selector %q: %v", selector, err)
	}
	listOptions := api.ListOptions{
		LabelSelector: l,
	}

	glog.Infof("Starting Service; polling interval=30s")
	for {

		for domain, s := range getDomainServiceMap(c, listOptions) {
			hn, err := serviceHostname(s.service)
			if err != nil {
				glog.Warningf("Couldn't find hostname for %s: %s", s.service.Name, err)
				continue
			}

			glog.Infof("Creating DNS record for %s service: %s -> %s", s.service.Name, domain, hn)
			elbZoneID, err := hostedZoneID(elbAPI, hn)
			if err != nil {
				glog.Warningf("Couldn't get zone ID: %s", err)
				continue
			}

			zone, err := getDestinationZone(domain, r53Api)
			if err != nil {
				glog.Warningf("Couldn't find destination zone: %s", err)
				continue
			}

			zoneID := *zone.Id
			zoneParts := strings.Split(zoneID, "/")
			zoneID = zoneParts[len(zoneParts) - 1]

			var rrs route53.ResourceRecordSet
			switch s.dnsRecordType {
			case A: rrs = makeATypeRecordSet(hn, elbZoneID, strings.TrimLeft(domain, "."), s.ttl)
			case CNAME: rrs = makeCNAMETypeRecordSet(hn, elbZoneID, strings.TrimLeft(domain, "."), s.ttl)
			}

			if err = updateDNS(r53Api, zoneID, rrs); err != nil {
				glog.Warning(err)
				continue
			// If no error and it was dryRun then log info about this
			} else if dryRun {
				glog.Infof("DRY RUN: We normally would have updated %s to point %s to %s (%s)", zoneID, domain, hn, elbZoneID)
			}

			glog.Infof("Created DNS record set: domain=%s, zoneID=%s", domain, zoneID)
		}
		time.Sleep(30 * time.Second)
	}
}

func getDomainServiceMap(c *client.Client, listOptions api.ListOptions) map[string]rule {
	result := make(map[string]rule)

	result = getServiceBasedDomainServiceMap(result, c, listOptions)
	result = getIngressBasedDomainServiceMap(result, c, listOptions)

	return result
}

func getServiceBasedDomainServiceMap(result map[string]rule, c *client.Client, listOptions api.ListOptions) map[string]rule {
	services, err := c.Services(api.NamespaceAll).List(listOptions)
	if err != nil {
		glog.Fatalf("Failed to list services: %v", err)
		return result
	}

	glog.Infof("Found %v DNS services in all namespaces with selector", len(services.Items))
	for _, service := range services.Items {
		dnsRecordType, ok := service.ObjectMeta.Annotations["dnsRecordType"]
		if !ok || !isDNSRecordTypeValid(dnsRecordType)  {
			dnsRecordType = defaultDNSRecordType()
		}

		ttl := defaultDNSRecordTTL()

		ttlString, ok := service.ObjectMeta.Annotations["dnsRecordTTL"]
		if ok && parseTTL(ttlString) != 0  {
			ttl = parseTTL(ttlString)
		}


		annotation, ok := service.ObjectMeta.Annotations["domainName"]
		if ok {
			domains := strings.Split(annotation, ",")
			for _, domain := range domains {
				result[domain] = rule { service: &service, dnsRecordType: dnsRecordType, ttl: ttl }
			}
		} else {
			glog.Warningf("Domain name not set for %s", service.Name)
		}

	}
	return result
}

func getIngressBasedDomainServiceMap(result map[string]rule, c *client.Client, listOptions api.ListOptions) map[string]rule {
	service := getIngressService(c)

	if service == nil {
		return result
	}

	ingresses, err := c.Ingress(api.NamespaceAll).List(listOptions)
	if err != nil {
		glog.Infof("Failed to list ingress: %v", err)
		return result
	}

	glog.Infof("Found %v DNS ingress in all namespaces", len(ingresses.Items))

	for _, ingress := range ingresses.Items {
		dnsRecordType, ok := ingress.ObjectMeta.Annotations["dnsRecordType"]
		if !ok || !isDNSRecordTypeValid(dnsRecordType)  {
			dnsRecordType = defaultDNSRecordType()
		}

		ttl := defaultDNSRecordTTL()

		ttlString, ok := ingress.ObjectMeta.Annotations["dnsRecordTTL"]
		if ok && parseTTL(ttlString) != 0  {
			ttl = parseTTL(ttlString)
		}

		// ingress
		annotation, ok := ingress.ObjectMeta.Annotations["domainName"]
		if ok {
			domains := strings.Split(annotation, ",")
			for _, domain := range domains {
				result[domain] = rule{ service: service, dnsRecordType: dnsRecordType, ttl: ttl }
			}
		} else {
			glog.Warningf("Domain name not set for %s", ingress.Name)
		}
	}
	return result
}

func defaultDNSRecordType() string {
	dnsRecordType := os.Getenv("DNS_RECORD_TYPE")
	if dnsRecordType == "" || !isDNSRecordTypeValid(dnsRecordType) {
		dnsRecordType = A
	}
	return dnsRecordType
}

func defaultDNSRecordTTL() int64 {
	ttl := parseTTL(os.Getenv("DNS_RECORD_TTL"))
	if ttl == 0  {
		ttl = 300
	}
	return ttl
}

func isDNSRecordTypeValid(dnsRecordType string) bool {
	return dnsRecordType == A || dnsRecordType == CNAME
}


func parseTTL(ttl string) int64 {
	result, err := strconv.Atoi(ttl)
	if err != nil {
		return 0
	} else if result <= 0 {
		return 0
	}
	return int64(result)
}

func getIngressService(c *client.Client)*api.Service {
	selector := os.Getenv("INGRESS_SERVICE_SELECTOR")
	if selector == "" {
		selector = "ingress=endpoint"
	}

	glog.Infof("Using selector %v to find service for ingress", selector)

	l, err := labels.Parse(selector)
	if err != nil {
		glog.Fatalf("Failed to parse selector %v: %v", selector, err)
		return nil
	}
	serviceListOptions := api.ListOptions{
		LabelSelector: l,
	}

	services, err := c.Services(api.NamespaceAll).List(serviceListOptions)
	if err != nil {
		glog.Infof("Something went wrong: %v", err)
		return nil
	}

	if len(services.Items) == 0 {
		glog.Infof("Ingress controller not installed or ingress service selector %v is not valid. SKIP", l)
		return nil
	}

	service := services.Items[0]
	glog.Infof("For ingress use service: %v", service.GenerateName)
	return &service
}

func getDestinationZone(domain string, r53Api *route53.Route53) (*route53.HostedZone, error) {
	tld, err := getTLD(domain)
	if err != nil {
		return nil, err
	}

	listHostedZoneInput := route53.ListHostedZonesByNameInput{
		DNSName: &tld,
	}
	hzOut, err := r53Api.ListHostedZonesByName(&listHostedZoneInput)
	if err != nil {
		return nil, fmt.Errorf("No zone found for %s: %v", tld, err)
	}
	// TODO: The AWS API may return multiple pages, we should parse them all

	return findMostSpecificZoneForDomain(domain, hzOut.HostedZones)
}

func findMostSpecificZoneForDomain(domain string, zones []*route53.HostedZone) (*route53.HostedZone, error) {
	domain = domainWithTrailingDot(domain)
	if len(zones) < 1 {
		return nil, fmt.Errorf("No zone found for %s", domain)
	}
	var mostSpecific *route53.HostedZone
	curLen := 0

	for i := range zones {
		zone := zones[i]
		zoneName := *zone.Name

		if strings.HasSuffix(domain, zoneName) && curLen < len(zoneName) {
			curLen = len(zoneName)
			mostSpecific = zone
		}
	}

	if mostSpecific == nil {
		return nil, fmt.Errorf("Zone found %s does not match domain given %s", *zones[0].Name, domain)
	}

	return mostSpecific, nil
}

func getTLD(domain string) (string, error) {
	domainParts := strings.Split(domain, ".")
	segments := len(domainParts)
	if segments < 3 {
		return "", fmt.Errorf("Domain %s is invalid - it should be a fully qualified domain name and subdomain (i.e. test.example.com)", domain)
	}
	return strings.Join(domainParts[segments - 2:], "."), nil
}

func domainWithTrailingDot(withoutDot string) string {
	if withoutDot[len(withoutDot) - 1:] == "." {
		return withoutDot
	}
	return fmt.Sprint(withoutDot, ".")
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

func loadBalancerNameFromHostname(hostname string) (string, error) {
	var name string
	hostnameSegments := strings.Split(hostname, "-")
	if len(hostnameSegments) < 2 {
		return name, fmt.Errorf("%s is not a valid ELB hostname", hostname)
	}
	name = hostnameSegments[0]

	// handle internal load balancer naming
	if name == "internal" {
		name = hostnameSegments[1]
	}

	return name, nil
}

func hostedZoneID(elbAPI *elb.ELB, hostname string) (string, error) {
	elbName, err := loadBalancerNameFromHostname(hostname)
	if err != nil {
		return "", fmt.Errorf("Couldn't parse ELB hostname: %v", err)
	}
	lbInput := &elb.DescribeLoadBalancersInput{
		LoadBalancerNames: []*string{
			&elbName,
		},
	}
	resp, err := elbAPI.DescribeLoadBalancers(lbInput)
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

func updateDNS(r53Api *route53.Route53, zoneID string, rrs route53.ResourceRecordSet) error {
	glog.Infof("ZoneID: %v", zoneID)

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
		HostedZoneId: &zoneID,
	}

	if dryRun {
		return nil
	}

	_, err := r53Api.ChangeResourceRecordSets(&crrsInput)
	if err != nil {
		return fmt.Errorf("Failed to update record set: %v", err)
	}
	return nil
}

func makeATypeRecordSet(hn, hzID, domain string, ttl int64) route53.ResourceRecordSet {
	at := route53.AliasTarget{
		DNSName:              &hn,
		EvaluateTargetHealth: aws.Bool(false),
		HostedZoneId:         &hzID,
	}
	return route53.ResourceRecordSet{
		AliasTarget: &at,
		Name:        &domain,
		Type:        aws.String("A"),
	}
}

func makeCNAMETypeRecordSet(hn, hzID, domain string, ttl int64) route53.ResourceRecordSet {
	return route53.ResourceRecordSet{
		ResourceRecords: []*route53.ResourceRecord{
			&route53.ResourceRecord{
				Value: &hn,
			},
		},
		Name:  &domain,
		Type:  aws.String("CNAME"),
		TTL:   aws.Int64(ttl),
	}
}
