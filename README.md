# Kubernetes => Route53 Mapping Service

This is a Kubernetes service that polls services (in all namespaces) that are configured
with the label `dns=route53` and adds the appropriate alias to the domain specified by
the annotation `domainName=sub.mydomain.io`. Multiple domains and top level domains are also supported:
`domainName=.mydomain.io,sub1.mydomain.io,sub2.mydomain.io`

# Usage

### route53-kubernetes ReplicationController

The following is an example ReplicationController definition for route53-kubernetes:

Create the ReplicationController via `kubectl create -f <name_of_route53-kubernetes-rc.yaml>`

Note: We don't currently sign our docker images. So, please use our images at your own risk.

```yaml
apiVersion: extensions/v1beta1
kind: Deployment
metadata:
  name: route53-kubernetes
  namespace: kube-system
  labels:
    app: route53-kubernetes
spec:
  replicas: 1
  template:
    metadata:
      labels:
        app: route53-kubernetes
    spec:
      containers:
        - image: quay.io/molecule/route53-kubernetes:v1.3.0
          name: route53-kubernetes
```

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

### Service Configuration

Given the following Kubernetes service definition:

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

An "A" record for `test.mydomain.com` will be created as an alias to the ELB that is
configured by kubernetes. This assumes that a hosted zone exists in Route53 for mydomain.com.
Any record that previously existed for that dns record will be updated.

#### Evaluating target health

Target Health Evaluation by Route 53 can be enabled by adding an `evaluateTargetHealth=true` annotation. Please note that
if you have specified multiple domains with the `domainName` annotation, target health evaluation will be enabled for
each record created.


### Alternative setup

This setup shows some alternative ways to configure route53-kubernetes. First, you can specify kubernetes certs manually if you do not have service accounts enabled. Second, access to AWS can be configured through a [Shared Credentials File](https://github.com/aws/aws-sdk-go/wiki/configuring-sdk).

```yaml
apiVersion: v1
kind: ReplicationController
metadata:
  name: route53-kubernetes
  namespace: kube-system
  labels:
    app: route53-kubernetes
spec:
  replicas: 1
  selector:
    app: route53-kubernetes
  template:
    metadata:
      labels:
        app: route53-kubernetes
    spec:
      volumes:
        - name: ssl-cert
          secret:
            secretName: kube-ssl
        - name: aws-creds
          secret:
            secretName: aws-creds
      containers:
        - image: quay.io/molecule/route53-kubernetes:v1.3.0
          name: route53-kubernetes
          volumeMounts:
            - name: ssl-cert
              mountPath: /opt/certs
              readOnly: true
            - name: aws-creds
              mountPath: /opt/creds
              readOnly: true
          env:
            - name: "CA_FILE_PATH"
              value: "/opt/certs/ca.pem"
            - name: "CERT_FILE_PATH"
              value: "/opt/certs/cert.pem"
            - name: "KEY_FILE_PATH"
              value: "/opt/certs/key.pem"
            - name: "AWS_SHARED_CREDENTIALS_FILE"
              value: "/opt/creds/credentials"
```

# Building locally

### Install dependencies

We use glide to manage dependencies. To fetch the dependencies to your local `vendor/` folder please run:
```bash
glide install -v
```

### Build the Image

You may choose to use Docker images for route53-kubernetes on our [Quay](https://quay.io/repository/molecule/route53-kubernetes?tab=tags) namespace or to build the binary, docker image, and push the docker image from scratch. See the [Makefile](https://github.com/wearemolecule/route53-kubernetes/blob/master/Makefile) for more information on doing this process manually.
