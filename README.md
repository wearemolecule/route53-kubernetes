# Kubernetes => Route53 Mapping Service

This is a Kubernetes service that polls services (in all namespaces) that are configured
with the label `dns=route53` and adds the appropriate alias to the domain specified by
the annotation `domainName=sub.mydomain.io`.

For example, give the below Kubernetes service definition:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: my-app
  labels:
    app: my-app
    role: web
    dns: route53
  annotations:
    domainName: "test.mydomain.com"
spec:
  selector:
    app: my-app
    role: web
  ports:
  - name: web
    port: 80
    protocol: TCP
    targetPort: web
  - name: web-ssl
    port: 443
    protocol: TCP
    targetPort: web-ssl
  type: LoadBalancer
```

This service will create an "A" record as an alias to the ELB that is configured by kubernetes.

This service expects that it's running on a Kubernetes node on AWS and that the IAM profile for
that node is set up to allow the following, along with the default permissions needed by Kubernetes:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": "route53:ListHostedZonesByName",
            "Resource": "*"
        },
        {
            "Effect": "Allow",
            "Action": "elasticloadbalancing:DescribeLoadBalancers",
            "Resource": "*"
        },
        {
            "Effect": "Allow",
            "Action": "route53:ChangeResourceRecordSets",
            "Resource": "*"
        }
    ]
}
```
