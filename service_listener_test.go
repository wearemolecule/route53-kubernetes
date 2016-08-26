package main

import "testing"

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
