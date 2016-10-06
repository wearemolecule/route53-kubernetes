package main

import (
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/route53"
)

func TestLoadBalancerNameFromHostname(t *testing.T) {
	scenarios := map[string]string{
		"testpublic-1111111111.us-east-1.elb.amazonaws.com":            "testpublic",
		"internal-testinternal-2222222222.us-east-1.elb.amazonaws.com": "testinternal",
	}

	for hostname, elbName := range scenarios {
		extractedName, err := loadBalancerNameFromHostname(hostname)
		if err != nil {
			t.Errorf("Expected %s to parse to %s, but got %v", hostname, elbName, err)
		}
		if extractedName != elbName {
			t.Errorf("Expected %s but got %s for hostname %s", elbName, extractedName, hostname)
		}
	}

	invalid := []string{
		"nodashes",
		"internal",
	}

	for _, bad := range invalid {
		extractedName, err := loadBalancerNameFromHostname(bad)
		if err == nil {
			t.Errorf("Expected %s to parse to fail, but got %v", bad, extractedName)
		}
	}
}

func TestFindMostSpecificZoneForDomain(t *testing.T) {
	demo := route53.HostedZone{
		Name: aws.String("demo.com."),
	}
	demoSub := route53.HostedZone{
		Name: aws.String("sub.demo.com."),
	}
	zones := []*route53.HostedZone{
		&demo,
		&demoSub,
	}

	scenarios := map[string]*route53.HostedZone{
		".demo.com":           &demo,
		"test.demo.com":       &demo,
		"test.again.demo.com": &demo,
		"sub.demo.com":        &demoSub,
		"test.sub.demo.com":   &demoSub,
	}

	for domain, expectedZone := range scenarios {
		actualZone, err := findMostSpecificZoneForDomain(domain, zones)
		if err != nil {
			t.Error("Expected no error to be raised", err)
			return
		}
		if actualZone != expectedZone {
			t.Errorf("Expected %s to eq %s for domain %s", *actualZone, *expectedZone, domain)
		}
	}

}

func TestDomainWithTrailingDot(t *testing.T) {
	scenarios := map[string]string{
		".test.com":         ".test.com.",
		"hello.goodbye.io.": "hello.goodbye.io.",
	}

	for withoutDot, withDot := range scenarios {
		result := domainWithTrailingDot(withoutDot)
		if result != withDot {
			t.Errorf("Expected %s but got %s for hostname %s", withDot, result, withoutDot)
		}
	}
}

func TestGetTLD(t *testing.T) {
	scenarios := map[string]string{
		".test.com":                            "test.com",
		"hello.goodbye.io":                     "goodbye.io",
		"this.is.really.long.hello.goodbye.io": "goodbye.io",
	}

	for domain, tld := range scenarios {
		result, err := getTLD(domain)
		if err != nil {
			t.Error("Unexpected error: ", err)
		}
		if result != tld {
			t.Errorf("Expected %s but got %s for tld %s", tld, result, domain)
		}
	}

	bad := "bad.domain"
	_, err := getTLD(bad)
	if err == nil {
		t.Errorf("%s should cause error", bad)
	}
}
