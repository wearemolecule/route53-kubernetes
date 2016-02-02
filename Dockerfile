FROM alpine

ENTRYPOINT ["/opt/app/route53-kubernetes"]
RUN mkdir -p /opt/app
WORKDIR /opt/app

ADD route53-kubernetes /opt/app/route53-kubernetes
