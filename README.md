# Kubernetes => Route53 Mapping Service

This is a Kubernetes service that polls services (in all namespaces) that are configured
with the label `dns=route53` and adds the appropriate alias to the domain specified by
the annotation `domainName=sub.mydomain.io`.

# Setup

### Install dependencies

We use glide to manage dependencies. To fetch the dependencies to your local `vendor/` folder please run:
```bash
glide install -v
```

### Build the Image

You may choose to use Docker images for route53-kubernetes on our [Quay](https://quay.io/repository/molecule/route53-kubernetes?tab=tags) namespace or to build the binary, docker image, and push the docker image from scratch. See the [Makefile](https://github.com/wearemolecule/route53-kubernetes/blob/master/Makefile) for more information on doing this process manually.

Note: Use our images at your own risk.

### route53-kubernetes ReplicationController

The following is an example ReplicationController definition for route53-kubernetes:

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
        - image: quay.io/molecule/route53-kubernetes:v1.1.3
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

Create the ReplicationController via `kubectl create -f <name_of_route53-kubernetes-rc.yaml>`

The following can be an easier alternative if you use IAM Role instead of [Shared Credentials File](https://github.com/aws/aws-sdk-go/wiki/configuring-sdk):

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
          hostPath:
            path: "/etc/kubernetes"
      containers:
        - image: quay.io/molecule/route53-kubernetes:v1.1.3
          imagePullPolicy: Always
          name: route53-kubernetes
          volumeMounts:
            - name: ssl-cert
              mountPath: /etc/kubernetes
              readOnly: true
          env:
            - name: "CA_FILE_PATH"
              value: "/etc/kubernetes/ssl/ca.pem"
            - name: "CERT_FILE_PATH"
              value: "/etc/kubernetes/ssl/worker.pem"
            - name: "KEY_FILE_PATH"
              value: "/etc/kubernetes/ssl/worker-key.pem"
            - name : "AWS_REGION"
              value: "ap-northeast-1"
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
